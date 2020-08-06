package mempool

import (
	"strconv"
	"unsafe"

	mpproto "github.com/tendermint/tendermint/proto/tendermint/mempool"
)

type Client struct {
	mp Mempool
}

func NewMempoolClient(memAddress string) *Client {
	ptrInt, _ := strconv.ParseUint(memAddress, 10, 64)
	ptrVal := uintptr(ptrInt)
	ptr := unsafe.Pointer(ptrVal)
	memPtr := (*Mempool)(ptr)
	cli := &Client{
		mp: *memPtr,
	}
	return cli
}

func (cli *Client) GetNextTransaction(
	req *mpproto.GetNextTransactionRequest) (*mpproto.GetNextTransactionResponse, error) {
	// todo mempool check
	tx := (cli.mp).GetNextTransaction(req.RemainingBytes, req.RemainingGas, req.Start)

	msg := mpproto.Message{
		Sum: &mpproto.Message_Tx{
			Tx: &mpproto.Tx{
				Tx: []byte(tx),
			},
		},
	}

	sts := mpproto.Status{
		Code:    0,
		Message: "TBD",
	}

	resp := mpproto.GetNextTransactionResponse{
		Status: &sts,
		TxMsg:  &msg,
	}

	return &resp, nil
}
