package colfer

import (
	"fmt"
	"math/big"
	"strings"
)

// evalPackageBounds compares ColferSizeMax and ColferListMax for a package
// pair. Expressions that the safe evaluator cannot reduce statically produce an
// unknown finding rather than a guess.
func (ctx *compatContext) evalPackageBounds(pkgName string, oldP, newP *Package, pr *PackageReport) {
	var oldLim, newLim Limits
	if ctx.oldBounds != nil {
		oldLim = ctx.oldBounds[pkgName]
	}
	if ctx.newBounds != nil {
		newLim = ctx.newBounds[pkgName]
	}

	ctx.compareBound(pkgName, oldLim.SizeMax, newLim.SizeMax, true, pr)
	ctx.compareBound(pkgName, oldLim.ListMax, newLim.ListMax, false, pr)
}

func (ctx *compatContext) compareBound(pkgName, oldExpr, newExpr string, size bool, pr *PackageReport) {
	if oldExpr == "" || newExpr == "" {
		return
	}

	oldVal, oldErr := evalLimitExpr(oldExpr)
	newVal, newErr := evalLimitExpr(newExpr)

	tightCode := CodeSizeMaxTightened
	unknownCode := CodeSizeMaxUnknown
	noun := "ColferSizeMax serial byte"
	if !size {
		tightCode = CodeListMaxTightened
		unknownCode = CodeListMaxUnknown
		noun = "ColferListMax element"
	}

	if oldErr != nil || newErr != nil {
		var bad string
		switch {
		case oldErr != nil && newErr != nil:
			bad = fmt.Sprintf("both expressions (%q and %q)", oldExpr, newExpr)
		case oldErr != nil:
			bad = fmt.Sprintf("the old expression %q", oldExpr)
		default:
			bad = fmt.Sprintf("the new expression %q", newExpr)
		}
		pr.Findings = append(pr.Findings, Finding{
			Code: unknownCode,
			Detail: fmt.Sprintf(
				"cannot statically prove the %s bound because %s could not be evaluated; treat the bound as unknown instead of assuming the limits match",
				noun, bad),
			OldToNew: VerdictUnknown,
			NewToOld: VerdictUnknown,
		})
		return
	}

	// Reader stricter than writer rejects legal serials.
	if cmp := newVal.Cmp(oldVal); cmp < 0 {
		pr.Findings = append(pr.Findings, boundFinding(tightCode, pkgName, oldExpr, newExpr, noun,
			"new reader bound "+newExpr+" is below old writer bound "+oldExpr+
				"; the generated unmarshal accepts a serial only while the byte index is strictly below the bound and otherwise returns ColferMax, so a serial near the old limit is rejected", true))
	} else if cmp > 0 {
		pr.Findings = append(pr.Findings, boundFinding(tightCode, pkgName, oldExpr, newExpr, noun,
			"old reader bound "+oldExpr+" is below new writer bound "+newExpr+
				"; the generated unmarshal accepts a serial only while the byte index is strictly below the bound and otherwise returns ColferMax, so a serial near the new limit is rejected by the old decoder", false))
	}
}

func boundFinding(code Code, pkgName, oldExpr, newExpr, noun, detail string, newReaderTighter bool) Finding {
	f := Finding{
		Code:   code,
		Struct: pkgName,
		Detail: detail + " (" + noun + " limit is enforced in the generated unmarshal)",
	}
	if newReaderTighter {
		f.OldToNew = VerdictIncompatible
		f.NewToOld = VerdictCompatible
	} else {
		f.OldToNew = VerdictCompatible
		f.NewToOld = VerdictIncompatible
	}
	return f
}

// evalLimitExpr evaluates a constant integer expression supporting decimal
// literals (with underscores), the binary shift operator and basic
// arithmetic. Any identifier or unsupported construct causes an error so the
// caller can mark the rule unknown.
func evalLimitExpr(expr string) (*big.Int, error) {
	tokens, err := tokenizeLimit(expr)
	if err != nil {
		return nil, err
	}
	p := &limitParser{tokens: tokens}
	v, err := p.parseExpr()
	if err != nil {
		return nil, err
	}
	if p.pos != len(p.tokens) {
		return nil, fmt.Errorf("unexpected token %q", p.tokens[p.pos].text)
	}
	return v, nil
}

type limitToken struct {
	kind string // "num", "op"
	text string
}

func tokenizeLimit(expr string) ([]limitToken, error) {
	var toks []limitToken
	for i := 0; i < len(expr); {
		c := expr[i]
		switch {
		case c == ' ' || c == '\t':
			i++
		case strings.IndexByte("0123456789_", c) >= 0:
			start := i
			for i < len(expr) && (isDigit(expr[i]) || expr[i] == '_') {
				i++
			}
			toks = append(toks, limitToken{"num", expr[start:i]})
		case strings.IndexByte("+-*/%()<>", c) >= 0:
			if c == '<' || c == '>' {
				if i+1 >= len(expr) || expr[i+1] != c {
					return nil, fmt.Errorf("unsupported operator at %d", i)
				}
				toks = append(toks, limitToken{"op", string([]byte{c, c})})
				i += 2
				continue
			}
			toks = append(toks, limitToken{"op", string(c)})
			i++
		default:
			return nil, fmt.Errorf("unsupported character %q", string(c))
		}
	}
	return toks, nil
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

type limitParser struct {
	tokens []limitToken
	pos    int
}

func (p *limitParser) peek() *limitToken {
	if p.pos >= len(p.tokens) {
		return nil
	}
	return &p.tokens[p.pos]
}

func (p *limitParser) parseExpr() (*big.Int, error) {
	return p.parseShift()
}

func (p *limitParser) parseShift() (*big.Int, error) {
	left, err := p.parseAddSub()
	if err != nil {
		return nil, err
	}
	for {
		t := p.peek()
		if t == nil || (t.text != "<<" && t.text != ">>") {
			return left, nil
		}
		p.pos++
		right, err := p.parseAddSub()
		if err != nil {
			return nil, err
		}
		if !right.IsInt64() || right.Sign() < 0 {
			return nil, fmt.Errorf("shift amount not a non-negative integer")
		}
		n := uint(right.Int64())
		if t.text == "<<" {
			left.Lsh(left, n)
		} else {
			left.Rsh(left, n)
		}
	}
}

func (p *limitParser) parseAddSub() (*big.Int, error) {
	left, err := p.parseMulDiv()
	if err != nil {
		return nil, err
	}
	for {
		t := p.peek()
		if t == nil || (t.text != "+" && t.text != "-") {
			return left, nil
		}
		p.pos++
		right, err := p.parseMulDiv()
		if err != nil {
			return nil, err
		}
		if t.text == "+" {
			left.Add(left, right)
		} else {
			left.Sub(left, right)
		}
	}
}

func (p *limitParser) parseMulDiv() (*big.Int, error) {
	left, err := p.parseUnary()
	if err != nil {
		return nil, err
	}
	for {
		t := p.peek()
		if t == nil || (t.text != "*" && t.text != "/" && t.text != "%") {
			return left, nil
		}
		p.pos++
		right, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		switch t.text {
		case "*":
			left.Mul(left, right)
		case "/":
			if right.Sign() == 0 {
				return nil, fmt.Errorf("division by zero")
			}
			left.Quo(left, right)
		default:
			if right.Sign() == 0 {
				return nil, fmt.Errorf("modulo by zero")
			}
			left.Mod(left, right)
		}
	}
}

func (p *limitParser) parseUnary() (*big.Int, error) {
	t := p.peek()
	if t == nil {
		return nil, fmt.Errorf("missing operand")
	}
	if t.text == "-" || t.text == "+" {
		p.pos++
		v, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		if t.text == "-" {
			v.Neg(v)
		}
		return v, nil
	}
	if t.text == "(" {
		p.pos++
		v, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		closing := p.peek()
		if closing == nil || closing.text != ")" {
			return nil, fmt.Errorf("missing closing parenthesis")
		}
		p.pos++
		return v, nil
	}
	if t.kind != "num" {
		return nil, fmt.Errorf("unsupported token %q (identifiers are not evaluated)", t.text)
	}
	p.pos++
	digits := strings.ReplaceAll(t.text, "_", "")
	v, ok := new(big.Int).SetString(digits, 10)
	if !ok {
		return nil, fmt.Errorf("invalid number %q", t.text)
	}
	return v, nil
}
