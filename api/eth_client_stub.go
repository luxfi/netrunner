package api

import (
	"context"
	"fmt"
	"math/big"

	ethereum "github.com/luxfi/geth"
	"github.com/luxfi/geth/common"
	"github.com/luxfi/geth/core/types"
)

// ethClientStub is a minimal stub implementation of EthClient for testing
type ethClientStub struct {
	ipAddr string
	port   uint
}

// NewEthClient creates a new EthClient stub for testing
// This is a minimal implementation that returns reasonable test values
func NewEthClient(ipAddr string, port uint) EthClient {
	return &ethClientStub{
		ipAddr: ipAddr,
		port:   port,
	}
}

// NewEthClientWithChainID creates a new EthClient stub with chain ID
func NewEthClientWithChainID(ipAddr string, port uint, chainID string) EthClient {
	// For now, we ignore chainID in the stub
	return NewEthClient(ipAddr, port)
}

// BalanceAt returns a non-zero balance for testing
func (c *ethClientStub) BalanceAt(ctx context.Context, account common.Address, blockNumber *big.Int) (*big.Int, error) {
	// Return 1000 ETH as a reasonable test balance
	balance := new(big.Int)
	balance.SetString("1000000000000000000000", 10) // 1000 ETH in wei
	return balance, nil
}

// Close does nothing in the stub
func (c *ethClientStub) Close() {
	// No-op
}

// AcceptedCallContract stub implementation
func (c *ethClientStub) AcceptedCallContract(ctx context.Context, msg ethereum.CallMsg) ([]byte, error) {
	return []byte{}, nil
}

// AcceptedCodeAt stub implementation
func (c *ethClientStub) AcceptedCodeAt(ctx context.Context, account common.Address) ([]byte, error) {
	return []byte{}, nil
}

// AcceptedNonceAt stub implementation
func (c *ethClientStub) AcceptedNonceAt(ctx context.Context, account common.Address) (uint64, error) {
	return 0, nil
}

// BlockByHash stub implementation
func (c *ethClientStub) BlockByHash(ctx context.Context, hash common.Hash) (*types.Block, error) {
	return nil, fmt.Errorf("BlockByHash not implemented in stub")
}

// BlockByNumber stub implementation
func (c *ethClientStub) BlockByNumber(ctx context.Context, number *big.Int) (*types.Block, error) {
	return nil, fmt.Errorf("BlockByNumber not implemented in stub")
}

// BlockNumber stub implementation
func (c *ethClientStub) BlockNumber(ctx context.Context) (uint64, error) {
	return 100, nil // Return a reasonable block number
}

// CallContract stub implementation
func (c *ethClientStub) CallContract(ctx context.Context, msg ethereum.CallMsg, blockNumber *big.Int) ([]byte, error) {
	return []byte{}, nil
}

// CodeAt stub implementation
func (c *ethClientStub) CodeAt(ctx context.Context, account common.Address, blockNumber *big.Int) ([]byte, error) {
	return []byte{}, nil
}

// EstimateGas stub implementation
func (c *ethClientStub) EstimateGas(ctx context.Context, msg ethereum.CallMsg) (uint64, error) {
	return 21000, nil // Return standard transfer gas
}

// FilterLogs stub implementation
func (c *ethClientStub) FilterLogs(ctx context.Context, q ethereum.FilterQuery) ([]types.Log, error) {
	return []types.Log{}, nil
}

// HeaderByNumber stub implementation
func (c *ethClientStub) HeaderByNumber(ctx context.Context, number *big.Int) (*types.Header, error) {
	return nil, fmt.Errorf("HeaderByNumber not implemented in stub")
}

// NonceAt stub implementation
func (c *ethClientStub) NonceAt(ctx context.Context, account common.Address, blockNumber *big.Int) (uint64, error) {
	return 0, nil
}

// SendTransaction stub implementation
func (c *ethClientStub) SendTransaction(ctx context.Context, tx *types.Transaction) error {
	return nil
}

// SubscribeFilterLogs stub implementation
func (c *ethClientStub) SubscribeFilterLogs(ctx context.Context, q ethereum.FilterQuery, ch chan<- types.Log) (ethereum.Subscription, error) {
	return nil, fmt.Errorf("SubscribeFilterLogs not implemented in stub")
}

// SuggestGasPrice stub implementation
func (c *ethClientStub) SuggestGasPrice(ctx context.Context) (*big.Int, error) {
	gasPrice := new(big.Int)
	gasPrice.SetString("1000000000", 10) // 1 Gwei
	return gasPrice, nil
}

// SuggestGasTipCap stub implementation
func (c *ethClientStub) SuggestGasTipCap(ctx context.Context) (*big.Int, error) {
	tipCap := new(big.Int)
	tipCap.SetString("1000000000", 10) // 1 Gwei
	return tipCap, nil
}

// TransactionReceipt stub implementation
func (c *ethClientStub) TransactionReceipt(ctx context.Context, txHash common.Hash) (*types.Receipt, error) {
	return nil, fmt.Errorf("TransactionReceipt not implemented in stub")
}
