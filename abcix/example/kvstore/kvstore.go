package kvstore

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"os"

	"github.com/tendermint/tendermint/libs/log"

	"github.com/tendermint/tendermint/abci/example/code"
	"github.com/tendermint/tendermint/abcix/types"
	"github.com/tendermint/tendermint/libs/rand"
	"github.com/tendermint/tendermint/version"
	dbm "github.com/tendermint/tm-db"
)

var (
	stateKey        = []byte("stateKey")
	kvPairPrefixKey = []byte("kvPairKey:")

	ProtocolVersion uint64 = 0x1
)

type State struct {
	db      dbm.DB
	Size    int64  `json:"size"`
	Height  int64  `json:"height"`
	AppHash []byte `json:"app_hash"`
	txPool  map[string]uint64
}

func loadState(db dbm.DB) State {
	var state State
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

func saveState(state State) {
	stateBytes, err := json.Marshal(state)
	if err != nil {
		panic(err)
	}
	err = state.db.Set(stateKey, stateBytes)
	if err != nil {
		panic(err)
	}
}

func prefixKey(key []byte) []byte {
	return append(kvPairPrefixKey, key...)
}

//---------------------------------------------------

var _ types.Application = (*Application)(nil)

type Application struct {
	types.BaseApplication
	state        State
	RetainBlocks int64 // blocks to retain after commit (via ResponseCommit.RetainHeight)
}

func NewApplication() *Application {
	state := loadState(dbm.NewMemDB())
	state.txPool = make(map[string]uint64)
	return &Application{state: state}
}

func (app *Application) Info(req types.RequestInfo) (resInfo types.ResponseInfo) {
	return types.ResponseInfo{
		Data:             fmt.Sprintf("{\"size\":%v}", app.state.Size),
		Version:          version.ABCIVersion,
		AppVersion:       ProtocolVersion,
		LastBlockHeight:  app.state.Height,
		LastBlockAppHash: app.state.AppHash,
	}
}

func (app *Application) CheckTx(req types.RequestCheckTx) types.ResponseCheckTx {
	var key string
	// Priority can only be set by directly calling CheckTx, not by RecheckTx
	if req.Type == types.CheckTxType_New {
		priority := rand.Uint64() % 100
		keyBytes, _ := getTxInfo(req.Tx)
		if bytes.Equal(keyBytes, []byte("xunan")) {
			priority = 100
		}
		key = string(keyBytes)
		app.state.txPool[key] = priority
	}
	return types.ResponseCheckTx{Code: code.CodeTypeOK, GasWanted: 1, Priority: app.state.txPool[key]}
}

// Iterate txs from mempool ordered by priority and select the ones to be included in the next block
func (app *Application) CreateBlock(
	req types.RequestCreateBlock,
	mempool *types.MempoolIter,
) types.ResponseCreateBlock {
	var txs [][]byte
	var size int64
	logger := log.NewTMLogger(log.NewSyncWriter(os.Stdout))

	maxBytes := int64(22020096)
	maxGas := int64(10)
	for mempool.HasNext() {
		tx, err := mempool.GetNextTransaction(maxBytes, maxGas)
		if err != nil {
			panic("failed to get next tx from mempool")
		}
		if tx == nil {
			break
		}
		maxBytes -= int64(len(tx))
		maxGas--

		key, _ := getTxInfo(tx)
		logger.Info("Tx list", "key", string(key), "priority", app.state.txPool[string(key)])
		txs = append(txs, tx)
		size++
	}
	appHash := make([]byte, 8)
	binary.PutVarint(appHash, size)

	events := []types.Event{
		{
			Type: "create_block",
			Attributes: []types.EventAttribute{
				{Key: []byte("height"), Value: []byte{byte(req.Height)}},
				{Key: []byte("valid tx"), Value: []byte{byte(len(txs))}},
			},
		},
	}

	return types.ResponseCreateBlock{Txs: txs, Hash: appHash, Events: events}
}

// Combination of ABCI.BeginBlock, []ABCI.DeliverTx, and ABCI.EndBlock
func (app *Application) DeliverBlock(req types.RequestDeliverBlock) types.ResponseDeliverBlock {
	ret := types.ResponseDeliverBlock{}
	// ABCI.DeliverTx, tx is either "key=value" or just arbitrary bytes
	for _, tx := range req.Txs {
		key, value := getTxInfo(tx)
		err := app.state.db.Set(prefixKey(key), value)
		if err != nil {
			panic(err)
		}
		app.state.Size++

		events := []types.Event{
			{
				Type: "app",
				Attributes: []types.EventAttribute{
					{Key: []byte("creator"), Value: []byte("Cosmoshi Netowoko"), Index: true},
					{Key: []byte("key"), Value: key, Index: true},
					{Key: []byte("index_key"), Value: []byte("index is working"), Index: true},
					{Key: []byte("noindex_key"), Value: []byte("index is working"), Index: false},
				},
			},
		}
		txResp := types.ResponseDeliverTx{Events: events}
		ret.DeliverTxs = append(ret.DeliverTxs, &txResp)
	}
	return ret
}

func (app *Application) Commit() types.ResponseCommit {
	// Using a memdb - just return the big endian size of the db
	appHash := make([]byte, 8)
	binary.PutVarint(appHash, app.state.Size)
	app.state.AppHash = appHash
	app.state.Height++
	saveState(app.state)

	resp := types.ResponseCommit{Data: appHash}
	if app.RetainBlocks > 0 && app.state.Height >= app.RetainBlocks {
		resp.RetainHeight = app.state.Height - app.RetainBlocks + 1
	}
	return resp
}

// Returns an associated value or nil if missing.
func (app *Application) Query(reqQuery types.RequestQuery) (resQuery types.ResponseQuery) {
	if reqQuery.Prove {
		value, err := app.state.db.Get(prefixKey(reqQuery.Data))
		if err != nil {
			panic(err)
		}
		if value == nil {
			resQuery.Log = "does not exist"
		} else {
			resQuery.Log = "exists"
		}
		resQuery.Index = -1 // TODO make Proof return index
		resQuery.Key = reqQuery.Data
		resQuery.Value = value
		resQuery.Height = app.state.Height

		return
	}

	resQuery.Key = reqQuery.Data
	value, err := app.state.db.Get(prefixKey(reqQuery.Data))
	if err != nil {
		panic(err)
	}
	if value == nil {
		resQuery.Log = "does not exist"
	} else {
		resQuery.Log = "exists"
	}
	resQuery.Value = value
	resQuery.Height = app.state.Height

	return resQuery
}

func getTxInfo(tx []byte) ([]byte, []byte) {
	var key, value []byte
	parts := bytes.Split(tx, []byte("="))
	if len(parts) == 2 {
		key, value = parts[0], parts[1]
	} else {
		key, value = tx, tx
	}
	return key, value
}
