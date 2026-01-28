package api

import (
	"fmt"

	"github.com/luxfi/sdk/admin"
	"github.com/luxfi/sdk/exchangevm"
	"github.com/luxfi/sdk/health"
	"github.com/luxfi/sdk/indexer"
	sdkinfo "github.com/luxfi/sdk/info"
	"github.com/luxfi/sdk/platformvm"
	// evmclient "github.com/luxfi/evm/plugin/evm/client"
)

// interface compliance
var (
	_ Client        = (*APIClient)(nil)
	_ NewAPIClientF = NewAPIClient
)

// APIClient gives access to most node apis (or suitable wrappers)
type APIClient struct {
	platform     *platformvm.Client
	xChain       *exchangevm.Client
	xChainWallet *exchangevm.WalletClient
	cChain       interface{} // evmclient.Client
	cChainEth    EthClient
	info         *sdkinfo.Client
	health       *health.Client
	admin        *admin.Client
	pindex       *indexer.Client
	cindex       *indexer.Client
}

// Returns a new API client for a node at [ipAddr]:[port].
type NewAPIClientF func(ipAddr string, port uint16) Client

// NewAPIClient initialize most of node apis
func NewAPIClient(ipAddr string, port uint16) Client {
	uri := fmt.Sprintf("http://%s:%d", ipAddr, port)
	// cChainClient := evmclient.NewClient(uri, "C")

	// Create client instances
	platformClient := platformvm.NewClient(uri)
	xChainClient := exchangevm.NewClient(uri, "X")
	xChainWalletClient := exchangevm.NewWalletClient(uri, "X")
	infoClient := sdkinfo.NewClient(uri)
	healthClient := health.NewClient(uri)
	adminClient := admin.NewClient(uri)
	pindexClient := indexer.NewClient(uri + "/ext/index/P/block")
	cindexClient := indexer.NewClient(uri + "/ext/index/C/block")

	return &APIClient{
		platform:     platformClient,
		xChain:       xChainClient,
		xChainWallet: xChainWalletClient,
		cChain:       nil, // cChainClient,
		cChainEth:    nil, // NewEthClient(ipAddr, uint(port)), // wrapper over ethclient.Client
		info:         infoClient,
		health:       healthClient,
		admin:        adminClient,
		pindex:       pindexClient,
		cindex:       cindexClient,
	}
}

func (c APIClient) PChainAPI() *platformvm.Client {
	return c.platform
}

func (c APIClient) XChainAPI() *exchangevm.Client {
	return c.xChain
}

func (c APIClient) XChainWalletAPI() *exchangevm.WalletClient {
	return c.xChainWallet
}

func (c APIClient) CChainAPI() interface{} { // evmclient.Client {
	return c.cChain
}

func (c APIClient) CChainEthAPI() EthClient {
	return c.cChainEth
}

func (c APIClient) InfoAPI() *sdkinfo.Client {
	return c.info
}

func (c APIClient) HealthAPI() HealthClient {
	return c.health
}

func (c APIClient) AdminAPI() *admin.Client {
	return c.admin
}

func (c APIClient) PChainIndexAPI() *indexer.Client {
	return c.pindex
}

func (c APIClient) CChainIndexAPI() *indexer.Client {
	return c.cindex
}
