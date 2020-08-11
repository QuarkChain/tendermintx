package proxy

import (
	abcicli "github.com/tendermint/tendermint/abci/client"
	"github.com/tendermint/tendermint/abci/types"
)

//go:generate mockery -case underscore -name AppConnConsensus|AppConnMempool|AppConnQuery|AppConnSnapshot

//----------------------------------------------------------------------------------------
// Enforce which abci msgs can be sent on a connection at the type level

type LegacyAppConnConsensus interface {
	SetResponseCallback(abcicli.Callback)
	Error() error

	InitChainSync(types.RequestInitChain) (*types.ResponseInitChain, error)

	BeginBlockSync(types.RequestBeginBlock) (*types.ResponseBeginBlock, error)
	DeliverTxAsync(types.RequestDeliverTx) *abcicli.ReqRes
	EndBlockSync(types.RequestEndBlock) (*types.ResponseEndBlock, error)
	CommitSync() (*types.ResponseCommit, error)
}

type LegacyAppConnMempool interface {
	SetResponseCallback(abcicli.Callback)
	Error() error

	CheckTxAsync(types.RequestCheckTx) *abcicli.ReqRes
	CheckTxSync(types.RequestCheckTx) (*types.ResponseCheckTx, error)

	FlushAsync() *abcicli.ReqRes
	FlushSync() error
}

type LegacyAppConnQuery interface {
	Error() error

	EchoSync(string) (*types.ResponseEcho, error)
	InfoSync(types.RequestInfo) (*types.ResponseInfo, error)
	QuerySync(types.RequestQuery) (*types.ResponseQuery, error)

	//	SetOptionSync(key string, value string) (res types.Result)
}

type LegacyAppConnSnapshot interface {
	Error() error

	ListSnapshotsSync(types.RequestListSnapshots) (*types.ResponseListSnapshots, error)
	OfferSnapshotSync(types.RequestOfferSnapshot) (*types.ResponseOfferSnapshot, error)
	LoadSnapshotChunkSync(types.RequestLoadSnapshotChunk) (*types.ResponseLoadSnapshotChunk, error)
	ApplySnapshotChunkSync(types.RequestApplySnapshotChunk) (*types.ResponseApplySnapshotChunk, error)
}

//-----------------------------------------------------------------------------------------
// Implements AppConnConsensus (subset of abcicli.Client)

type legacySppConnConsensus struct {
	appConn abcicli.Client
}

func NewLegacyAppConnConsensus(appConn abcicli.Client) LegacyAppConnConsensus {
	return &legacySppConnConsensus{
		appConn: appConn,
	}
}

func (app *legacySppConnConsensus) SetResponseCallback(cb abcicli.Callback) {
	app.appConn.SetResponseCallback(cb)
}

func (app *legacySppConnConsensus) Error() error {
	return app.appConn.Error()
}

func (app *legacySppConnConsensus) InitChainSync(req types.RequestInitChain) (*types.ResponseInitChain, error) {
	return app.appConn.InitChainSync(req)
}

func (app *legacySppConnConsensus) BeginBlockSync(req types.RequestBeginBlock) (*types.ResponseBeginBlock, error) {
	return app.appConn.BeginBlockSync(req)
}

func (app *legacySppConnConsensus) DeliverTxAsync(req types.RequestDeliverTx) *abcicli.ReqRes {
	return app.appConn.DeliverTxAsync(req)
}

func (app *legacySppConnConsensus) EndBlockSync(req types.RequestEndBlock) (*types.ResponseEndBlock, error) {
	return app.appConn.EndBlockSync(req)
}

func (app *legacySppConnConsensus) CommitSync() (*types.ResponseCommit, error) {
	return app.appConn.CommitSync()
}

//------------------------------------------------
// Implements AppConnMempool (subset of abcicli.Client)

type legacyAppConnMempool struct {
	appConn abcicli.Client
}

func NewLegacyAppConnMempool(appConn abcicli.Client) LegacyAppConnMempool {
	return &legacyAppConnMempool{
		appConn: appConn,
	}
}

func (app *legacyAppConnMempool) SetResponseCallback(cb abcicli.Callback) {
	app.appConn.SetResponseCallback(cb)
}

func (app *legacyAppConnMempool) Error() error {
	return app.appConn.Error()
}

func (app *legacyAppConnMempool) FlushAsync() *abcicli.ReqRes {
	return app.appConn.FlushAsync()
}

func (app *legacyAppConnMempool) FlushSync() error {
	return app.appConn.FlushSync()
}

func (app *legacyAppConnMempool) CheckTxAsync(req types.RequestCheckTx) *abcicli.ReqRes {
	return app.appConn.CheckTxAsync(req)
}

func (app *legacyAppConnMempool) CheckTxSync(req types.RequestCheckTx) (*types.ResponseCheckTx, error) {
	return app.appConn.CheckTxSync(req)
}

//------------------------------------------------
// Implements AppConnQuery (subset of abcicli.Client)

type legacyAppConnQuery struct {
	appConn abcicli.Client
}

func NewLegacyAppConnQuery(appConn abcicli.Client) LegacyAppConnQuery {
	return &legacyAppConnQuery{
		appConn: appConn,
	}
}

func (app *legacyAppConnQuery) Error() error {
	return app.appConn.Error()
}

func (app *legacyAppConnQuery) EchoSync(msg string) (*types.ResponseEcho, error) {
	return app.appConn.EchoSync(msg)
}

func (app *legacyAppConnQuery) InfoSync(req types.RequestInfo) (*types.ResponseInfo, error) {
	return app.appConn.InfoSync(req)
}

func (app *legacyAppConnQuery) QuerySync(reqQuery types.RequestQuery) (*types.ResponseQuery, error) {
	return app.appConn.QuerySync(reqQuery)
}

//------------------------------------------------
// Implements AppConnSnapshot (subset of abcicli.Client)

type legacyAppConnSnapshot struct {
	appConn abcicli.Client
}

func NewLegacyAppConnSnapshot(appConn abcicli.Client) LegacyAppConnSnapshot {
	return &legacyAppConnSnapshot{
		appConn: appConn,
	}
}

func (app *legacyAppConnSnapshot) Error() error {
	return app.appConn.Error()
}

func (app *legacyAppConnSnapshot) ListSnapshotsSync(req types.RequestListSnapshots) (*types.ResponseListSnapshots, error) {
	return app.appConn.ListSnapshotsSync(req)
}

func (app *legacyAppConnSnapshot) OfferSnapshotSync(req types.RequestOfferSnapshot) (*types.ResponseOfferSnapshot, error) {
	return app.appConn.OfferSnapshotSync(req)
}

func (app *legacyAppConnSnapshot) LoadSnapshotChunkSync(
	req types.RequestLoadSnapshotChunk) (*types.ResponseLoadSnapshotChunk, error) {
	return app.appConn.LoadSnapshotChunkSync(req)
}

func (app *legacyAppConnSnapshot) ApplySnapshotChunkSync(
	req types.RequestApplySnapshotChunk) (*types.ResponseApplySnapshotChunk, error) {
	return app.appConn.ApplySnapshotChunkSync(req)
}
