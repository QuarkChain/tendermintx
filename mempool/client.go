package mempool

import (
	"unsafe"

	mempoolproto "github.com/tendermint/tendermint/proto/tendermint/mempool"
)

type Client struct {
	mp Mempool
}

func NewMempoolClient(memInt int64) *Client {
	ptrVal := uintptr(memInt)
	ptr := unsafe.Pointer(ptrVal)
	memPtr := (*Mempool)(ptr)
	cli := &Client{
		mp: *memPtr,
	}
	return cli
}

func (cli *Client) GetNextTransaction(
	req *mempoolproto.GetNextTransactionRequest) (*mempoolproto.GetNextTransactionResponse, error) {
	tx := (cli.mp).GetNextTransaction(req.RemainingBytes, req.RemainingGas, req.Start)
	var errorCode int32 = 1
	errorMessage := "Failed"
	if tx == nil {
		errorCode = 0
		errorMessage = "Succeed"
	}

	msg := mempoolproto.Message{
		Sum: &mempoolproto.Message_Tx{
			Tx: &mempoolproto.Tx{
				Tx: []byte(tx),
			},
		},
	}

	sts := mempoolproto.Status{
		Code:    errorCode,
		Message: errorMessage,
	}

	resp := mempoolproto.GetNextTransactionResponse{
		Status: &sts,
		TxMsg:  &msg,
	}

	return &resp, nil
}
