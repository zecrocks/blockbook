package zec

import (
	"encoding/json"
	"errors"
	"math/big"
	"strconv"

	"github.com/golang/glog"
	"github.com/martinboehm/btcd/wire"
	"github.com/martinboehm/btcutil/chaincfg"
	"github.com/trezor/blockbook/bchain"
	"github.com/trezor/blockbook/bchain/coins/btc"
	"github.com/trezor/blockbook/common"
)

const (
	// MainnetMagic is mainnet network constant
	MainnetMagic wire.BitcoinNet = 0x6427e924
	// TestnetMagic is testnet network constant
	TestnetMagic wire.BitcoinNet = 0xbff91afa
	// RegtestMagic is regtest network constant
	RegtestMagic wire.BitcoinNet = 0x5f3fe8aa
)

var (
	// MainNetParams are parser parameters for mainnet
	MainNetParams chaincfg.Params
	// TestNetParams are parser parameters for testnet
	TestNetParams chaincfg.Params
	// RegtestParams are parser parameters for regtest
	RegtestParams chaincfg.Params
)

func init() {
	MainNetParams = chaincfg.MainNetParams
	MainNetParams.Net = MainnetMagic

	// Address encoding magics
	MainNetParams.AddressMagicLen = 2
	MainNetParams.PubKeyHashAddrID = []byte{0x1C, 0xB8} // base58 prefix: t1
	MainNetParams.ScriptHashAddrID = []byte{0x1C, 0xBD} // base58 prefix: t3

	TestNetParams = chaincfg.TestNet3Params
	TestNetParams.Net = TestnetMagic

	// Address encoding magics
	TestNetParams.AddressMagicLen = 2
	TestNetParams.PubKeyHashAddrID = []byte{0x1D, 0x25} // base58 prefix: tm
	TestNetParams.ScriptHashAddrID = []byte{0x1C, 0xBA} // base58 prefix: t2

	RegtestParams = chaincfg.RegressionNetParams
	RegtestParams.Net = RegtestMagic
}

// JoinSplit represents a JoinSplit description in Zcash Sprout transactions
type JoinSplit struct {
	VPubOld common.JSONNumber `json:"vpub_old"`
	VPubNew common.JSONNumber `json:"vpub_new"`
}

// ZCashSpecificData contains Zcash-specific transaction fields for shielded pools
type ZCashSpecificData struct {
	VJoinSplit          []JoinSplit       `json:"vjoinsplit,omitempty"`
	ValueBalance        common.JSONNumber `json:"valueBalance,omitempty"`        // Net value balance (Sapling, can be positive or negative)
	ValueBalanceZat     common.JSONNumber `json:"valueBalanceZat,omitempty"`     // Net value balance in zatoshis
	ValueBalanceSapling common.JSONNumber `json:"valueBalanceSapling,omitempty"` // Legacy field name (for compatibility)
	ValueBalanceOrchard common.JSONNumber `json:"valueBalanceOrchard,omitempty"` // Orchard pool balance (if present)
}

// ZCashParser handle
type ZCashParser struct {
	*btc.BitcoinLikeParser
	baseparser *bchain.BaseParser
}

// NewZCashParser returns new ZCashParser instance
func NewZCashParser(params *chaincfg.Params, c *btc.Configuration) *ZCashParser {
	return &ZCashParser{
		BitcoinLikeParser: btc.NewBitcoinLikeParser(params, c),
		baseparser:        &bchain.BaseParser{},
	}
}

// GetChainParams contains network parameters for the main ZCash network,
// the regression test ZCash network, the test ZCash network and
// the simulation test ZCash network, in this order
func GetChainParams(chain string) *chaincfg.Params {
	if !chaincfg.IsRegistered(&MainNetParams) {
		err := chaincfg.Register(&MainNetParams)
		if err == nil {
			err = chaincfg.Register(&TestNetParams)
		}
		if err == nil {
			err = chaincfg.Register(&RegtestParams)
		}
		if err != nil {
			panic(err)
		}
	}
	switch chain {
	case "test":
		return &TestNetParams
	case "regtest":
		return &RegtestParams
	default:
		return &MainNetParams
	}
}

// PackTx packs transaction to byte array using protobuf
func (p *ZCashParser) PackTx(tx *bchain.Tx, height uint32, blockTime int64) ([]byte, error) {
	return p.baseparser.PackTx(tx, height, blockTime)
}

// UnpackTx unpacks transaction from protobuf byte array
func (p *ZCashParser) UnpackTx(buf []byte) (*bchain.Tx, uint32, error) {
	return p.baseparser.UnpackTx(buf)
}

// ParseTxFromJson parses JSON message containing transaction and returns Tx struct
// This method extends Bitcoin's ParseTxFromJson to handle Zcash-specific shielded pool fields
func (p *ZCashParser) ParseTxFromJson(msg json.RawMessage) (*bchain.Tx, error) {
	// First, parse using the standard Bitcoin parser
	tx, err := p.BitcoinLikeParser.ParseTxFromJson(msg)
	if err != nil {
		return nil, err
	}

	// Parse Zcash-specific fields for shielded pools
	var zcashData ZCashSpecificData
	err = json.Unmarshal(msg, &zcashData)
	if err != nil {
		return nil, err
	}

	// Calculate the shielded pool contribution to fee calculation
	shieldedPoolValue := big.NewInt(0)

	// Debug logging
	glog.Infof("ZCash ParseTxFromJson: txid=%s, vjoinsplit=%d, valueBalance=%s, valueBalanceZat=%s, valueBalanceSapling=%s, valueBalanceOrchard=%s",
		tx.Txid, len(zcashData.VJoinSplit), zcashData.ValueBalance, zcashData.ValueBalanceZat, zcashData.ValueBalanceSapling, zcashData.ValueBalanceOrchard)

	// Process JoinSplit descriptions (Sprout shielded pool)
	// Note: vpub_old and vpub_new are already in zatoshis, not decimal ZEC
	for _, js := range zcashData.VJoinSplit {
		// vpub_new: value entering transparent pool (treated as input)
		if js.VPubNew != "" {
			vpubNew := new(big.Int)
			if _, ok := vpubNew.SetString(string(js.VPubNew), 10); !ok {
				return nil, errors.New("failed to parse vpub_new")
			}
			shieldedPoolValue.Add(shieldedPoolValue, vpubNew)
		}

		// vpub_old: value leaving transparent pool (treated as output)
		if js.VPubOld != "" {
			vpubOld := new(big.Int)
			if _, ok := vpubOld.SetString(string(js.VPubOld), 10); !ok {
				return nil, errors.New("failed to parse vpub_old")
			}
			shieldedPoolValue.Sub(shieldedPoolValue, vpubOld)
		}
	}

	// Process valueBalanceZat (primary field - value balance in zatoshis)
	// This represents the net value balance for Sapling shielded pool
	// Positive values = net transfer OUT of shielded pool (treated as input)
	// Negative values = net transfer INTO shielded pool (treated as output)
	if zcashData.ValueBalanceZat != "" {
		valueBalance := new(big.Int)
		if _, ok := valueBalance.SetString(string(zcashData.ValueBalanceZat), 10); !ok {
			return nil, errors.New("failed to parse valueBalanceZat")
		}
		shieldedPoolValue.Add(shieldedPoolValue, valueBalance)
	} else if zcashData.ValueBalance != "" {
		// Fallback to valueBalance if valueBalanceZat is not present
		// Need to convert from decimal ZEC to zatoshis (multiply by 100000000)
		valueBalanceFloat, err := strconv.ParseFloat(string(zcashData.ValueBalance), 64)
		if err != nil {
			return nil, errors.New("failed to parse valueBalance as float")
		}
		valueBalanceSat := big.NewInt(int64(valueBalanceFloat * 100000000))
		shieldedPoolValue.Add(shieldedPoolValue, valueBalanceSat)
	}

	// Process legacy Sapling pool balance field (for compatibility)
	if zcashData.ValueBalanceSapling != "" {
		saplingBalance := new(big.Int)
		if _, ok := saplingBalance.SetString(string(zcashData.ValueBalanceSapling), 10); !ok {
			return nil, errors.New("failed to parse valueBalanceSapling")
		}
		shieldedPoolValue.Add(shieldedPoolValue, saplingBalance)
	}

	// Process Orchard pool balance (already in zatoshis)
	if zcashData.ValueBalanceOrchard != "" {
		orchardBalance := new(big.Int)
		if _, ok := orchardBalance.SetString(string(zcashData.ValueBalanceOrchard), 10); !ok {
			return nil, errors.New("failed to parse valueBalanceOrchard")
		}
		shieldedPoolValue.Add(shieldedPoolValue, orchardBalance)
	}

	// Store the shielded pool value in CoinSpecificData for use in fee calculation
	// We'll store both the original JSON and our calculated shielded pool value
	coinSpecificData := map[string]interface{}{
		"rawJson":           msg,
		"shieldedPoolValue": shieldedPoolValue,
	}
	tx.CoinSpecificData = coinSpecificData

	glog.Infof("ZCash ParseTxFromJson: txid=%s, final shieldedPoolValue=%s", tx.Txid, shieldedPoolValue.String())

	return tx, nil
}
