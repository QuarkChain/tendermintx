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

const (
	info = iota
	setoption
	query
	checktx
	initchain
	listsnapshots
	offersnapshot
	loadsnapshotchunk
	applysnapshotchunk
	beginblock
	endblock
)

type abciTypePair struct {
	request reflect.Type
	method  interface{}
}

var typeRegistry = map[int]abciTypePair{
	info:               {reflect.TypeOf(abci.RequestInfo{}), abci.Application.Info},
	setoption:          {reflect.TypeOf(abci.RequestSetOption{}), abci.Application.SetOption},
	query:              {reflect.TypeOf(abci.RequestQuery{}), abci.Application.Query},
	checktx:            {reflect.TypeOf(abci.RequestCheckTx{}), abci.Application.CheckTx},
	initchain:          {reflect.TypeOf(abci.RequestInitChain{}), abci.Application.InitChain},
	listsnapshots:      {reflect.TypeOf(abci.RequestListSnapshots{}), abci.Application.ListSnapshots},
	offersnapshot:      {reflect.TypeOf(abci.RequestOfferSnapshot{}), abci.Application.OfferSnapshot},
	loadsnapshotchunk:  {reflect.TypeOf(abci.RequestLoadSnapshotChunk{}), abci.Application.LoadSnapshotChunk},
	applysnapshotchunk: {reflect.TypeOf(abci.RequestApplySnapshotChunk{}), abci.Application.ApplySnapshotChunk},
	beginblock:         {reflect.TypeOf(abci.RequestBeginBlock{}), abci.Application.BeginBlock},
	endblock:           {reflect.TypeOf(abci.RequestEndBlock{}), abci.Application.EndBlock},
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

func (app *adaptedApp) applyLegacyABCI(req interface{}, resp interface{}, abciType int) error {
	typePair, ok := typeRegistry[abciType]
	if !ok {
		return errors.New("ABCI type registry not found")
	}
	abciReq := reflect.New(typePair.request).Interface()
	if err := copier.Copy(abciReq, req); err != nil {
		return err
	}

	abciResp := Apply(typePair.method, &app.abciApp, abciReq).Interface()
	if err := copier.Copy(resp, abciResp); err != nil {
		return err
	}
	return nil
}

func (app *adaptedApp) Info(req abcix.RequestInfo) (resp abcix.ResponseInfo) {
	if err := app.applyLegacyABCI(&req, &resp, info); err != nil {
		panic(err)
	}
	return
}

func (app *adaptedApp) SetOption(req abcix.RequestSetOption) (resp abcix.ResponseSetOption) {
	if err := app.applyLegacyABCI(&req, &resp, setoption); err != nil {
		panic("failed to adapt the ABCI legacy methods: " + err.Error())
	}
	return
}

func (app *adaptedApp) Query(req abcix.RequestQuery) (resp abcix.ResponseQuery) {
	if err := app.applyLegacyABCI(&req, &resp, query); err != nil {
		panic("failed to adapt the ABCI legacy methods: " + err.Error())
	}
	return
}

func (app *adaptedApp) CheckTx(req abcix.RequestCheckTx) (resp abcix.ResponseCheckTx) {
	if err := app.applyLegacyABCI(&req, &resp, checktx); err != nil {
		panic("failed to adapt the ABCI legacy methods: " + err.Error())
	}
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
	if err := app.applyLegacyABCI(&req, &resp, initchain); err != nil {
		panic("failed to adapt the ABCI legacy methods: " + err.Error())
	}
	return
}

func (app *adaptedApp) DeliverBlock(req abcix.RequestDeliverBlock) (resp abcix.ResponseDeliverBlock) {
	if err := app.applyLegacyABCI(&req, &resp, beginblock); err != nil {
		panic("failed to adapt the ABCI legacy methods: " + err.Error())
	}
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

	if err := app.applyLegacyABCI(&req, &resp, endblock); err != nil {
		panic("failed to adapt the ABCI legacy methods: " + err.Error())
	}
	endEvents := resp.Events

	allEvents := make([]abcix.Event, 0, len(beginEvents)+len(endEvents))
	allEvents = append(allEvents, beginEvents...)
	allEvents = append(allEvents, endEvents...)
	resp.Events = allEvents
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

func (app *adaptedApp) CheckBlock(req abcix.RequestCheckBlock) (resp abcix.ResponseCheckBlock) {
	// TODO
	return
}

func (app *adaptedApp) ListSnapshots(req abcix.RequestListSnapshots) (resp abcix.ResponseListSnapshots) {
	if err := app.applyLegacyABCI(&req, &resp, listsnapshots); err != nil {
		panic("failed to adapt the ABCI legacy methods: " + err.Error())
	}
	return
}

func (app *adaptedApp) OfferSnapshot(req abcix.RequestOfferSnapshot) (resp abcix.ResponseOfferSnapshot) {
	if err := app.applyLegacyABCI(&req, &resp, offersnapshot); err != nil {
		panic("failed to adapt the ABCI legacy methods: " + err.Error())
	}
	return
}

func (app *adaptedApp) LoadSnapshotChunk(req abcix.RequestLoadSnapshotChunk) (resp abcix.ResponseLoadSnapshotChunk) {
	if err := app.applyLegacyABCI(&req, &resp, loadsnapshotchunk); err != nil {
		panic("failed to adapt the ABCI legacy methods: " + err.Error())
	}
	return
}

func (app *adaptedApp) ApplySnapshotChunk(req abcix.RequestApplySnapshotChunk) (resp abcix.ResponseApplySnapshotChunk) {
	if err := app.applyLegacyABCI(&req, &resp, applysnapshotchunk); err != nil {
		panic("failed to adapt the ABCI legacy methods: " + err.Error())
	}
	return
}

func AdaptToABCIx(abciApp abci.Application) abcix.Application {
	return &adaptedApp{abciApp: abciApp}
}
