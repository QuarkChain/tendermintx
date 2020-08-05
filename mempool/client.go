package mempool

import (
	"github.com/pkg/errors"
	mempoolproto "github.com/tendermint/tendermint/proto/tendermint/mempool"
)

//todo use Mempool instead of clisMempool
type mempoolClient struct {
	mp *Mempool
}

func NewMempoolClient(mp *Mempool) *mempoolClient {
	cli := &mempoolClient{
		mp: mp,
	}
	return cli
}


func (cli *mempoolClient) GetNextTransaction(
	req *mempoolproto.GetNextTransactionRequest) (*mempoolproto.GetNextTransactionResponse, error) {
	remainByte := req.RemainingBytes
	remainGas := req.RemainingGas
	prior := req.Start
	// todo mempool check
	tx := (*cli.mp).GetNextTransaction(remainByte,remainGas,prior)

	Tx := mempoolproto.Tx{Tx: tx}

	mTx := mempoolproto.Message_Tx{Tx: &Tx}

	msg := mempoolproto.Message{
		Mtx: &mTx,
		Sum: nil,
	}
	sts := mempoolproto.Status{
		Code:    0,
		Message: "TBD",
	}

	return &mempoolproto.GetNextTransactionResponse{&sts,&msg}, errors.New("not finished")
}
