package adapter

import (
	"testing"
	"time"

	"github.com/magiconair/properties/assert"
	abci "github.com/tendermint/tendermint/abci/types"
	abcix "github.com/tendermint/tendermint/abcix/types"
)

type mockAbciApp struct {
	abci.BaseApplication
}

type mockAbcixApp struct {
	abcix.BaseApplication
}

func (app *mockAbcixApp) CheckTx(req abcix.RequestCheckTx) abcix.ResponseCheckTx {
	time.Sleep(1e8)
	return abcix.ResponseCheckTx{
		Code: abci.CodeTypeOK,
		Data: []byte("42"),
	}
}

func (app *mockAbciApp) CheckTx(req abci.RequestCheckTx) abci.ResponseCheckTx {
	time.Sleep(1e8)
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
	var events = []abci.Event{{Type: "begin"}}
	return abci.ResponseBeginBlock{Events: events}
}

func (app *mockAbciApp) EndBlock(req abci.RequestEndBlock) abci.ResponseEndBlock {
	var events = []abci.Event{{Type: "end"}}
	return abci.ResponseEndBlock{Events: events}
}

func TestAdapt(t *testing.T) {
	abciApp := &mockAbciApp{}
	app := AdaptToABCIx(abciApp)

	respCheckTx := app.CheckTx(abcix.RequestCheckTx{})
	assert.Equal(t, respCheckTx.Code, abci.CodeTypeOK)
	assert.Equal(t, respCheckTx.Data, []byte("42"))

	txs := [][]byte{{42}, {97}}
	respDeliverBlock := app.DeliverBlock(abcix.RequestDeliverBlock{Txs: txs})
	assert.Equal(t, respDeliverBlock.DeliverTxs[0].Data, txs[0])
	assert.Equal(t, respDeliverBlock.DeliverTxs[1].Data, txs[1])

	events := []abcix.Event{{Type: "begin"}, {Type: "end"}}
	assert.Equal(t, respDeliverBlock.Events, events)
}

func BenchmarkAdaptedApp_CheckTx(b *testing.B) {
	abciApp := &mockAbciApp{}
	app := AdaptToABCIx(abciApp)
	app.CheckTx(abcix.RequestCheckTx{})
}

func BenchmarkAdaptedApp_CheckTx2(b *testing.B) {
	abcixApp := &mockAbcixApp{}
	abcixApp.CheckTx(abcix.RequestCheckTx{})
}
