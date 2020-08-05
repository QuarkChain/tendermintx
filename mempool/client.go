package mempool

import (
	"github.com/pkg/errors"
	mempoolproto "github.com/tendermint/tendermint/proto/tendermint/mempool"
)

//todo use Mempool instead of clisMempool
type mempoolClient struct {
	cm *CListMempool
}

func NewMempoolClient(cm *CListMempool) *mempoolClient {
	cli := &mempoolClient{
		cm: cm,
	}
	return cli
}

func (cli *mempoolClient) GetNextTransaction(
	req *mempoolproto.GetNextTransactionRequest) (*mempoolproto.GetNextTransactionResponse, error) {
	//remainByte := req.RemainingBytes
	//remainGas := req.RemainingGas
	//prior := req.Start
	// todo mempool check, return tx
	//tx := cli.cm.GetNextTransaction(remainByte,remainGas,prior)

	return &mempoolproto.GetNextTransactionResponse{}, errors.New("not implemented")
}
