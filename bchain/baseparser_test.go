//go:build unittest

package bchain

import (
	"math/big"
	"testing"

	"github.com/trezor/blockbook/common"
)

func NewBaseParser(adp int) *BaseParser {
	return &BaseParser{
		AmountDecimalPoint: adp,
	}
}

var amounts = []struct {
	a           *big.Int
	s           string
	adp         int
	alternative string
}{
	{big.NewInt(123456789), "1.23456789", 8, "!"},
	{big.NewInt(2), "0.00000002", 8, "!"},
	{big.NewInt(300000000), "3", 8, "!"},
	{big.NewInt(498700000), "4.987", 8, "!"},
	{big.NewInt(567890), "0.00000000000056789", 18, "!"},
	{big.NewInt(-100000000), "-1", 8, "!"},
	{big.NewInt(-8), "-0.00000008", 8, "!"},
	{big.NewInt(-89012345678), "-890.12345678", 8, "!"},
	{big.NewInt(-12345), "-0.00012345", 8, "!"},
	{big.NewInt(12345678), "0.123456789012", 8, "0.12345678"},                       // test of truncation of too many decimal places
	{big.NewInt(12345678), "0.0000000000000000000000000000000012345678", 1234, "!"}, // test of too big number decimal places
	{big.NewInt(987), "9.87e-6", 8, "!"},                                             // test scientific notation (9.87 * 10^-6 * 10^8 = 987)
	{big.NewInt(123400000), "1.234e0", 8, "!"},                                       // test scientific notation (1.234 * 10^0 * 10^8 = 123400000)
	{big.NewInt(1234000000), "1.234e1", 8, "!"},                                      // test scientific notation (1.234 * 10^1 * 10^8 = 1234000000)
	{big.NewInt(12), "1.234e-7", 8, "!"},                                             // test scientific notation (1.234 * 10^-7 * 10^8 = 12.34, truncated to 12)
}

func TestBaseParser_AmountToDecimalString(t *testing.T) {
	for _, tt := range amounts {
		t.Run(tt.s, func(t *testing.T) {
			if got := NewBaseParser(tt.adp).AmountToDecimalString(tt.a); got != tt.s && got != tt.alternative {
				t.Errorf("BaseParser.AmountToDecimalString() = %v, want %v", got, tt.s)
			}
		})
	}
}

func TestBaseParser_AmountToBigInt(t *testing.T) {
	for _, tt := range amounts {
		t.Run(tt.s, func(t *testing.T) {
			got, err := NewBaseParser(tt.adp).AmountToBigInt(common.JSONNumber(tt.s))
			if err != nil {
				t.Errorf("BaseParser.AmountToBigInt() error = %v", err)
				return
			}
			if got.Cmp(tt.a) != 0 {
				t.Errorf("BaseParser.AmountToBigInt() = %v, want %v", got, tt.a)
			}
		})
	}
}
