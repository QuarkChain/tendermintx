package mempool

import (
	"github.com/pkg/errors"
	mempoolproto "github.com/tendermint/tendermint/proto/tendermint/mempool"
)

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
	// todo mempool check
	tx := (*cli.mp).GetNextTransaction(req.RemainingBytes,req.RemainingGas,req.Start)

	msg := mempoolproto.Message{
		Sum: &mempoolproto.Message_Tx{
			Tx: &mempoolproto.Tx{
				Tx: []byte(tx),
			},
		},
	}

	sts := mempoolproto.Status{
		Code:    0,
		Message: "TBD",
	}

	return &mempoolproto.GetNextTransactionResponse{&sts,&msg}, errors.New("not finished")
}
