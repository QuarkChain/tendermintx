package adapter

import (
	"errors"
	"reflect"

	"github.com/jinzhu/copier"

	abci "github.com/tendermint/tendermint/abci/types"
	abcix "github.com/tendermint/tendermint/abcix/types"
)

type adaptedApp struct {
	abciApp abci.Application
}

type AdaptedApp interface {
	OriginalApp() abci.Application
}

func (app *adaptedApp) OriginalApp() abci.Application {
	return app.abciApp
}

var typeRegistry = make(map[string]reflect.Type)

func registerType(s string, i interface{}) {
	typeRegistry[s] = reflect.TypeOf(i)
}

func newStruct(name string) (interface{}, bool) {
	elem, ok := typeRegistry[name]
	if !ok {
		return nil, false
	}
	return reflect.New(elem).Interface(), true
}

func Apply(f interface{}, args ...interface{}) reflect.Value {
	fun := reflect.ValueOf(f)
	in := make([]reflect.Value, len(args))
	for k, arg := range args {
		in[k] = reflect.ValueOf(arg).Elem()
	}
	r := fun.Call(in)
	return r[0]
}

func adaptGeneralization(req interface{}, resp interface{}, abciReq string, f interface{}) error {
	abcireq, ok := newStruct(abciReq)
	if !ok {
		return errors.New("fail")
	}
	if err := copier.Copy(abcireq, req); err != nil {
		// TODO: panic for debugging purposes. better error handling soon!
		panic(err)
	}

	abciResp := Apply(f, abcireq).Interface()
	if err := copier.Copy(resp, abciResp); err != nil {
		// TODO: panic for debugging purposes. better error handling soon!
		panic(err)
	}
	return nil
}

func (app *adaptedApp) Info(req abcix.RequestInfo) (resp abcix.ResponseInfo) {
	registerType("abci.RequestInfo", abci.RequestInfo{})
	adaptGeneralization(&req, &resp, "abci.RequestInfo", app.abciApp.Info)
	return
}

func (app *adaptedApp) SetOption(req abcix.RequestSetOption) (resp abcix.ResponseSetOption) {
	registerType("abci.RequestSetOption", abci.RequestSetOption{})
	adaptGeneralization(&req, &resp, "abci.RequestSetOption", app.abciApp.SetOption)
	return
}

func (app *adaptedApp) Query(req abcix.RequestQuery) (resp abcix.ResponseQuery) {
	registerType("abci.RequestQuery", abci.RequestQuery{})
	adaptGeneralization(&req, &resp, "abci.RequestQuery", app.abciApp.Query)
	return
}

func (app *adaptedApp) CheckTx(req abcix.RequestCheckTx) (resp abcix.ResponseCheckTx) {
	abciReq := abci.RequestCheckTx{}
	if err := copier.Copy(&abciReq, &req); err != nil {
		// TODO: panic for debugging purposes. better error handling soon!
		panic(err)
	}
	abciResp := app.abciApp.CheckTx(abciReq)
	if err := copier.Copy(&resp, &abciResp); err != nil {
		// TODO: panic for debugging purposes. better error handling soon!
		panic(err)
	}
	return
}

func (app *adaptedApp) CreateBlock(req abcix.RequestCreateBlock, iter *abcix.MempoolIter) abcix.ResponseCreateBlock {
	// TODO: defer to consensus engine for now
	panic("implement me")
}

func (app *adaptedApp) InitChain(req abcix.RequestInitChain) (resp abcix.ResponseInitChain) {
	registerType("abci.RequestInitChain", abci.RequestInitChain{})
	adaptGeneralization(&req, &resp, "abci.RequestInitChain", app.abciApp.InitChain)
	return
}

func (app *adaptedApp) DeliverBlock(req abcix.RequestDeliverBlock) (resp abcix.ResponseDeliverBlock) {
	var reqBegin abci.RequestBeginBlock
	if err := copier.Copy(&reqBegin, &req); err != nil {
		// TODO: panic for debugging purposes. better error handling soon!
		panic(err)
	}
	respBegin := app.abciApp.BeginBlock(reqBegin)
	events := respBegin.Events
	if err := copier.Copy(&resp, &respBegin); err != nil {
		// TODO: panic for debugging purposes. better error handling soon!
		panic(err)
	}

	for _, tx := range req.Txs {
		legacyRespDeliverTx := app.abciApp.DeliverTx(abci.RequestDeliverTx{Tx: tx})
		var respDeliverTx abcix.ResponseDeliverTx
		if err := copier.Copy(&respDeliverTx, &legacyRespDeliverTx); err != nil {
			// TODO: panic for debugging purposes. better error handling soon!
			panic(err)
		}
		resp.DeliverTxs = append(resp.DeliverTxs, &respDeliverTx)
	}

	var reqEnd abci.RequestEndBlock
	if err := copier.Copy(&reqEnd, &req); err != nil {
		// TODO: panic for debugging purposes. better error handling soon!
		panic(err)
	}
	respEnd := app.abciApp.EndBlock(reqEnd)
	events = append(events, respEnd.Events...)
	if err := copier.Copy(&resp, &respEnd); err != nil {
		// TODO: panic for debugging purposes. better error handling soon!
		panic(err)
	}

	// Reform the events
	resp.Events = make([]abcix.Event, len(events))
	for i, oldEvent := range events {
		oldEvent := oldEvent
		var newEvent abcix.Event
		if err := copier.Copy(&newEvent, &oldEvent); err != nil {
			// TODO: panic for debugging purposes. better error handling soon!
			panic(err)
		}
		resp.Events[i] = newEvent
	}

	return resp
}

func (app *adaptedApp) Commit() (resp abcix.ResponseCommit) {
	abciResp := app.abciApp.Commit()
	if err := copier.Copy(&resp, &abciResp); err != nil {
		// TODO: panic for debugging purposes. better error handling soon!
		panic(err)
	}
	return
}

func (app *adaptedApp) CheckBlock(req abcix.RequestCheckBlock) abcix.ResponseCheckBlock {
	// TODO: defer to consensus engine for now
	panic("implement me")
}

func (app *adaptedApp) ListSnapshots(req abcix.RequestListSnapshots) (resp abcix.ResponseListSnapshots) {
	registerType("abci.RequestListSnapshots", abci.RequestListSnapshots{})
	adaptGeneralization(&req, &resp, "abci.RequestListSnapshots", app.abciApp.ListSnapshots)
	return
}

func (app *adaptedApp) OfferSnapshot(req abcix.RequestOfferSnapshot) (resp abcix.ResponseOfferSnapshot) {
	registerType("abci.RequestOfferSnapshot", abci.RequestOfferSnapshot{})
	adaptGeneralization(&req, &resp, "abci.RequestOfferSnapshot", app.abciApp.OfferSnapshot)
	return
}

func (app *adaptedApp) LoadSnapshotChunk(req abcix.RequestLoadSnapshotChunk) (resp abcix.ResponseLoadSnapshotChunk) {
	registerType("abci.RequestLoadSnapshotChunk", abci.RequestLoadSnapshotChunk{})
	adaptGeneralization(&req, &resp, "abci.RequestLoadSnapshotChunk", app.abciApp.LoadSnapshotChunk)
	return
}

func (app *adaptedApp) ApplySnapshotChunk(req abcix.RequestApplySnapshotChunk) (resp abcix.ResponseApplySnapshotChunk) {
	registerType("abci.RequestApplySnapshotChunk", abci.RequestApplySnapshotChunk{})
	adaptGeneralization(&req, &resp, "abci.RequestApplySnapshotChunk", app.abciApp.ApplySnapshotChunk)
	return
}

func AdaptToABCIx(abciApp abci.Application) abcix.Application {
	return &adaptedApp{abciApp: abciApp}
}
