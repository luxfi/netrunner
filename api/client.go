package api

import (
	"context"
	"math/big"

	ethereum "github.com/luxfi/geth"
	"github.com/luxfi/geth/common"
	"github.com/luxfi/geth/core/types"
	"github.com/luxfi/sdk/api/admin"
	"github.com/luxfi/sdk/api/health"
	"github.com/luxfi/sdk/api/indexer"
	"github.com/luxfi/sdk/api/info"
	"github.com/luxfi/rpc"
	"github.com/luxfi/vm/vms/exchangevm"
	"github.com/luxfi/vm/vms/platformvm"
	// evmclient "github.com/luxfi/evm/plugin/evm/client"
)

// HealthClient is the interface for health API client operations
type HealthClient interface {
	Readiness(ctx context.Context, tags []string, options ...rpc.Option) (*health.APIReply, error)
	Health(ctx context.Context, tags []string, options ...rpc.Option) (*health.APIReply, error)
	Liveness(ctx context.Context, tags []string, options ...rpc.Option) (*health.APIReply, error)
}

// EthClient is the interface for ethereum client operations
type EthClient interface {
	// Balance operations
	BalanceAt(ctx context.Context, account common.Address, blockNumber *big.Int) (*big.Int, error)

	// Block operations
	BlockByHash(ctx context.Context, hash common.Hash) (*types.Block, error)
	BlockByNumber(ctx context.Context, number *big.Int) (*types.Block, error)
	BlockNumber(ctx context.Context) (uint64, error)
	HeaderByNumber(ctx context.Context, number *big.Int) (*types.Header, error)

	// Transaction operations
	SendTransaction(ctx context.Context, tx *types.Transaction) error
	TransactionReceipt(ctx context.Context, txHash common.Hash) (*types.Receipt, error)

	// Contract operations
	CallContract(ctx context.Context, msg ethereum.CallMsg, blockNumber *big.Int) ([]byte, error)
	AcceptedCallContract(ctx context.Context, msg ethereum.CallMsg) ([]byte, error)
	CodeAt(ctx context.Context, account common.Address, blockNumber *big.Int) ([]byte, error)
	AcceptedCodeAt(ctx context.Context, account common.Address) ([]byte, error)

	// Nonce operations
	NonceAt(ctx context.Context, account common.Address, blockNumber *big.Int) (uint64, error)
	AcceptedNonceAt(ctx context.Context, account common.Address) (uint64, error)

	// Gas operations
	EstimateGas(ctx context.Context, msg ethereum.CallMsg) (uint64, error)
	SuggestGasPrice(ctx context.Context) (*big.Int, error)
	SuggestGasTipCap(ctx context.Context) (*big.Int, error)

	// Log operations
	FilterLogs(ctx context.Context, q ethereum.FilterQuery) ([]types.Log, error)
	SubscribeFilterLogs(ctx context.Context, q ethereum.FilterQuery, ch chan<- types.Log) (ethereum.Subscription, error)

	// Connection management
	Close()
}

// Issues API calls to a node
// TODO: byzantine api. check if appropriate. improve implementation.
type Client interface {
	PChainAPI() *platformvm.Client
	XChainAPI() *exchangevm.Client
	XChainWalletAPI() *exchangevm.WalletClient
	CChainAPI() interface{}  // evmclient.Client
	CChainEthAPI() EthClient // ethclient websocket wrapper that adds mutexed calls, and lazy conn init (on first call)
	InfoAPI() *info.Client
	HealthAPI() HealthClient
	AdminAPI() *admin.Client
	PChainIndexAPI() *indexer.Client
	CChainIndexAPI() *indexer.Client
	// TODO add methods
}
