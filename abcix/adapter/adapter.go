package adapter

import (
	"encoding/json"
	"errors"
	"io/ioutil"
	"math"
	"os"
	"reflect"

	dbm "github.com/tendermint/tm-db"

	"github.com/jinzhu/copier"
	abci "github.com/tendermint/tendermint/abci/types"
	abcix "github.com/tendermint/tendermint/abcix/types"
	"github.com/tendermint/tendermint/libs/log"
	tdtypes "github.com/tendermint/tendermint/types"
)

var (
	maxBytes = tdtypes.DefaultConsensusParams().Block.MaxBytes
	maxGas   = tdtypes.DefaultConsensusParams().Block.MaxGas

	stateKey = []byte("adaptorStateKey")
)

type state struct {
	db      dbm.DB
	AppHash []byte        `json:"appHash"`
	Events  []abcix.Event `json:"events"`
}

type adaptedApp struct {
	abciApp abci.Application
	state   state
	logger  log.Logger
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
		maxGas = int64(math.MaxInt64)

	}
	// Update remainBytes based on previous block
	remainBytes := CalcRemainBytes(req)
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
		resp.DeliverTxs = append(resp.DeliverTxs, &abcix.ResponseDeliverTx{Code: abcix.CodeTypeOK})
	}
	resp.AppHash = app.state.AppHash
	resp.Events = app.state.Events
	return resp
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
	app.state.Events = allEvents
	saveState(app.state, app.logger)
	return resp
}

func (app *adaptedApp) Commit() (resp abcix.ResponseCommit) {
	abciResp := app.abciApp.Commit()
	if err := copier.Copy(&resp, &abciResp); err != nil {
		// TODO: panic for debugging purposes. better error handling soon!
		panic(err)
	}

	app.state.AppHash = resp.Data
	saveState(app.state, app.logger)
	return
}

func (app *adaptedApp) CheckBlock(req abcix.RequestCheckBlock) abcix.ResponseCheckBlock {
	respDeliverTx := make([]*abcix.ResponseDeliverTx, len(req.Txs))
	for i := range respDeliverTx {
		respDeliverTx[i] = &abcix.ResponseDeliverTx{Code: 0}
	}
	return abcix.ResponseCheckBlock{
		AppHash:    app.state.AppHash,
		DeliverTxs: respDeliverTx,
		Events:     app.state.Events,
	}
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

func AdaptToABCIx(abciApp abci.Application, optionDB ...dbm.DB) abcix.Application {
	var db dbm.DB
	switch len(optionDB) {
	case 0:
		dir, err := ioutil.TempDir(os.TempDir(), "adaptor")
		if err != nil {
			panic("failed to generate DB dir")
		}
		db, err = dbm.NewGoLevelDB("adaptor", dir)
		if err != nil {
			panic("failed to generate adaptor DB")
		}
	case 1:
		db = optionDB[0]
	default:
		panic("wrong options")

	}
	state := loadState(db)
	logger := log.NewTMLogger(log.NewSyncWriter(os.Stdout))
	return &adaptedApp{abciApp: abciApp, state: state, logger: logger}
}

func loadState(db dbm.DB) state {
	var state state
	state.db = db
	stateBytes, err := db.Get(stateKey)
	if err != nil {
		panic(err)
	}
	if len(stateBytes) == 0 {
		return state
	}
	err = json.Unmarshal(stateBytes, &state)
	if err != nil {
		panic(err)
	}
	return state
}

func saveState(state state, logger log.Logger) {
	stateBytes, err := json.Marshal(state)
	if err != nil {
		logger.Error("failed to marshal state", "err", err)
	}
	err = state.db.Set(stateKey, stateBytes)
	if err != nil {
		logger.Error("failed to save state", "err", err)
	}
}

func CalcRemainBytes(req abcix.RequestCreateBlock) int64 {
	remainBytes := maxBytes -
		tdtypes.MaxOverheadForBlock -
		tdtypes.MaxHeaderBytes -
		int64(len(req.LastCommitInfo.Votes))*tdtypes.MaxVoteBytes -
		int64(len(req.ByzantineValidators))*tdtypes.MaxEvidenceBytes
	return remainBytes
}
