package adapter

import (
	"errors"
	"reflect"

	"github.com/jinzhu/copier"
	tdypes "github.com/tendermint/tendermint/types"

	abci "github.com/tendermint/tendermint/abci/types"
	abcix "github.com/tendermint/tendermint/abcix/types"
)

var (
	maxBytes            = tdypes.DefaultConsensusParams().Block.MaxBytes
	maxGas              = tdypes.DefaultConsensusParams().Block.MaxGas
	maxOverheadForBlock = tdypes.MaxOverheadForBlock
	// MaxHeaderBytes is a maximum header size.
	maxHeaderBytes = tdypes.MaxHeaderBytes
	// MaxVoteBytes is a maximum vote size (including amino overhead).
	maxVoteBytes = tdypes.MaxVoteBytes
	// MaxEvidenceBytes is a maximum size of any evidence (including amino overhead).
	maxEvidenceBytes = tdypes.MaxEvidenceBytes
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

var typeRegistry = map[string]reflect.Type{
	"abci.RequestInfo":               reflect.TypeOf(abci.RequestInfo{}),
	"abci.RequestSetOption":          reflect.TypeOf(abci.RequestSetOption{}),
	"abci.RequestQuery":              reflect.TypeOf(abci.RequestQuery{}),
	"abci.RequestCheckTx":            reflect.TypeOf(abci.RequestCheckTx{}),
	"abci.RequestInitChain":          reflect.TypeOf(abci.RequestInitChain{}),
	"abci.RequestListSnapshots":      reflect.TypeOf(abci.RequestListSnapshots{}),
	"abci.RequestOfferSnapshot":      reflect.TypeOf(abci.RequestOfferSnapshot{}),
	"abci.RequestLoadSnapshotChunk":  reflect.TypeOf(abci.RequestOfferSnapshot{}),
	"abci.RequestApplySnapshotChunk": reflect.TypeOf(abci.RequestApplySnapshotChunk{}),
	"abci.RequestEndBlock":           reflect.TypeOf(abci.RequestEndBlock{}),
	"abci.RequestBeginBlock":         reflect.TypeOf(abci.RequestBeginBlock{}),
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
	elem, ok := typeRegistry[abciReq]
	if !ok {
		return errors.New("fail to build a new struct")
	}
	abcireq := reflect.New(elem).Interface()
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
	adaptGeneralization(&req, &resp, "abci.RequestInfo", app.abciApp.Info)
	return
}

func (app *adaptedApp) SetOption(req abcix.RequestSetOption) (resp abcix.ResponseSetOption) {
	adaptGeneralization(&req, &resp, "abci.RequestSetOption", app.abciApp.SetOption)
	return
}

func (app *adaptedApp) Query(req abcix.RequestQuery) (resp abcix.ResponseQuery) {
	adaptGeneralization(&req, &resp, "abci.RequestQuery", app.abciApp.Query)
	return
}

func (app *adaptedApp) CheckTx(req abcix.RequestCheckTx) (resp abcix.ResponseCheckTx) {
	adaptGeneralization(&req, &resp, "abci.RequestCheckTx", app.abciApp.CheckTx)
	return
}

func (app *adaptedApp) CreateBlock(
	req abcix.RequestCreateBlock,
	iter *abcix.MempoolIter,
) (resp abcix.ResponseCreateBlock) {
	if maxGas < 0 {
		maxGas = 1<<(64-1) - 1
	}
	// Update remainBytes based on previous block
	remainBytes := maxBytes -
		maxOverheadForBlock -
		maxHeaderBytes -
		int64(len(req.LastCommitInfo.Votes))*maxVoteBytes -
		int64(len(req.ByzantineValidators))*maxEvidenceBytes
	remainGas := maxGas

	for {
		tx, err := iter.GetNextTransaction(remainBytes, remainGas)
		// TODO: this in real world may never happen, and in the future we may need to handle this error more elegantly
		if err != nil {
			panic("failed to get next tx from mempool")
		}
		if len(tx) == 0 {
			break
		}
		resp.Txs = append(resp.Txs, tx)
		remainBytes -= int64(len(tx))
		remainGas--
	}
	return
}

func (app *adaptedApp) InitChain(req abcix.RequestInitChain) (resp abcix.ResponseInitChain) {
	adaptGeneralization(&req, &resp, "abci.RequestInitChain", app.abciApp.InitChain)
	return
}

func (app *adaptedApp) DeliverBlock(req abcix.RequestDeliverBlock) (resp abcix.ResponseDeliverBlock) {
	adaptGeneralization(&req, &resp, "abci.RequestBeginBlock", app.abciApp.BeginBlock)
	beginEvents := resp.Events

	for _, tx := range req.Txs {
		legacyRespDeliverTx := app.abciApp.DeliverTx(abci.RequestDeliverTx{Tx: tx})
		var respDeliverTx abcix.ResponseDeliverTx
		if err := copier.Copy(&respDeliverTx, &legacyRespDeliverTx); err != nil {
			// TODO: panic for debugging purposes. better error handling soon!
			panic(err)
		}
		// Adapt tx result to success. Note the underlying ABCI should be responsible to
		// record actual tx result for querying
		respDeliverTx.Code = abcix.CodeTypeOK
		resp.DeliverTxs = append(resp.DeliverTxs, &respDeliverTx)
	}

	adaptGeneralization(&req, &resp, "abci.RequestEndBlock", app.abciApp.EndBlock)
	endEvents := resp.Events

	resp.Events = make([]abcix.Event, len(beginEvents)+len(endEvents))
	copy(resp.Events, beginEvents)
	resp.Events = append(resp.Events, endEvents...)
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
	adaptGeneralization(&req, &resp, "abci.RequestListSnapshots", app.abciApp.ListSnapshots)
	return
}

func (app *adaptedApp) OfferSnapshot(req abcix.RequestOfferSnapshot) (resp abcix.ResponseOfferSnapshot) {
	adaptGeneralization(&req, &resp, "abci.RequestOfferSnapshot", app.abciApp.OfferSnapshot)
	return
}

func (app *adaptedApp) LoadSnapshotChunk(req abcix.RequestLoadSnapshotChunk) (resp abcix.ResponseLoadSnapshotChunk) {
	adaptGeneralization(&req, &resp, "abci.RequestLoadSnapshotChunk", app.abciApp.LoadSnapshotChunk)
	return
}

func (app *adaptedApp) ApplySnapshotChunk(req abcix.RequestApplySnapshotChunk) (resp abcix.ResponseApplySnapshotChunk) {
	adaptGeneralization(&req, &resp, "abci.RequestApplySnapshotChunk", app.abciApp.ApplySnapshotChunk)
	return
}

func AdaptToABCIx(abciApp abci.Application) abcix.Application {
	return &adaptedApp{abciApp: abciApp}
}
