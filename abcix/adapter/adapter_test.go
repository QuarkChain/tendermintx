package adapter

import (
	"testing"

	"github.com/magiconair/properties/assert"
	abci "github.com/tendermint/tendermint/abci/types"
	abcix "github.com/tendermint/tendermint/abcix/types"
)

type mockAbciApp struct {
	abci.BaseApplication
}

func (app *mockAbciApp) CheckTx(req abci.RequestCheckTx) abci.ResponseCheckTx {
	return abci.ResponseCheckTx{
		Code: abci.CodeTypeOK,
		Data: []byte("42"),
	}
}

func (app *mockAbciApp) DeliverTx(req abci.RequestDeliverTx) abci.ResponseDeliverTx {
	return abci.ResponseDeliverTx{
		Data: req.Tx,
	}
}

func (app *mockAbciApp) BeginBlock(req abci.RequestBeginBlock) abci.ResponseBeginBlock {
	var events = make([]abci.Event, 1)
	events[0] = abci.Event{Type: "begin"}
	return abci.ResponseBeginBlock{Events: events}
}

func (app *mockAbciApp) EndBlock(req abci.RequestEndBlock) abci.ResponseEndBlock {
	var events = make([]abci.Event, 1)
	events[0] = abci.Event{Type: "end"}
	return abci.ResponseEndBlock{Events: events}
}

func TestAdapt(t *testing.T) {
	abciApp := &mockAbciApp{}
	app := AdaptToABCIx(abciApp)

	respCheckTx := app.CheckTx(abcix.RequestCheckTx{})
	assert.Equal(t, respCheckTx.Code, abci.CodeTypeOK)
	assert.Equal(t, respCheckTx.Data, []byte("42"))

	var Txs = make([][]byte, 2)
	Txs[0] = []byte("42")
	Txs[1] = []byte("97")
	respDeliverBlock := app.DeliverBlock(abcix.RequestDeliverBlock{Txs: Txs})
	assert.Equal(t, respDeliverBlock.DeliverTxs[0].Data, Txs[0])
	assert.Equal(t, respDeliverBlock.DeliverTxs[1].Data, Txs[1])

	var events = make([]abcix.Event, 2)
	events[0] = abcix.Event{Type: "begin"}
	events[1] = abcix.Event{Type: "end"}
	assert.Equal(t, respDeliverBlock.Events, events)
}
