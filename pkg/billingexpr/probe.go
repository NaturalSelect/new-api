package billingexpr

import (
	"fmt"
	"math"
)

type UnitPrices struct {
	Request float64
	Tokens  map[string]float64
}

// NOTE: len is excluded on purpose; it only drives tier conditions and is held at 0 while probing.
var pricedVars = []string{"p", "c", "cr", "cc", "cc1h", "img", "img_o", "ai", "ao"}

func setTokenVar(params *TokenParams, name string, value float64) {
	switch name {
	case "p":
		params.P = value
	case "c":
		params.C = value
	case "cr":
		params.CR = value
	case "cc":
		params.CC = value
	case "cc1h":
		params.CC1h = value
	case "img":
		params.Img = value
	case "img_o":
		params.ImgO = value
	case "ai":
		params.AI = value
	case "ao":
		params.AO = value
	}
}

// NOTE: Every value shares the expression output scale, so divide by 1,000,000 to get USD.
// With len at 0 the default tier is read, and with an empty request every request rule is "not matched".
func ProbeUnitPrices(exprStr string) (UnitPrices, error) {
	base, _, err := RunExpr(exprStr, TokenParams{})
	if err != nil {
		return UnitPrices{}, err
	}
	if !isFinite(base) {
		return UnitPrices{}, fmt.Errorf("probe: expression is not finite at zero tokens")
	}

	used := UsedVars(exprStr)
	prices := UnitPrices{Request: base, Tokens: make(map[string]float64)}
	for _, name := range pricedVars {
		if !used[name] {
			continue
		}
		var params TokenParams
		setTokenVar(&params, name, 1)
		out, _, err := RunExpr(exprStr, params)
		if err != nil {
			return UnitPrices{}, fmt.Errorf("probe %s: %w", name, err)
		}
		if !isFinite(out) {
			return UnitPrices{}, fmt.Errorf("probe %s: expression is not finite", name)
		}
		prices.Tokens[name] = out - base
	}
	return prices, nil
}

func isFinite(v float64) bool {
	return !math.IsNaN(v) && !math.IsInf(v, 0)
}
