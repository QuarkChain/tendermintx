package mempool

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"io/ioutil"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gogo/protobuf/proto"
	gogotypes "github.com/gogo/protobuf/types"
	tmrand "github.com/tendermint/tendermint/libs/rand"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tendermint/tendermint/abci/example/counter"
	kvstore2 "github.com/tendermint/tendermint/abci/example/kvstore"
	abci "github.com/tendermint/tendermint/abci/types"
	"github.com/tendermint/tendermint/abcix/example/kvstore"
	abcix "github.com/tendermint/tendermint/abcix/types"
	cfg "github.com/tendermint/tendermint/config"
	"github.com/tendermint/tendermint/libs/log"
	"github.com/tendermint/tendermint/proxy"
	"github.com/tendermint/tendermint/types"
)

// A cleanupFunc cleans up any config / test files created for a particular
// test.
type cleanupFunc func()

type mpEnum int

const (
	enumclistmempool mpEnum = iota
	enumllrbmempool
	enumbtreemempool
)

type mempoolGen func(
	*cfg.MempoolConfig,
	proxy.AppConnMempool,
	int64,
	bool,
...Option,
) Mempool

var (
	mempoolFactory = map[mpEnum]mempoolGen{
		enumclistmempool: NewCListMempool,
		enumllrbmempool:  NewLLRBMempool,
		enumbtreemempool: NewBTreeMempool,
	}
	mpEnums = []mpEnum{enumclistmempool, enumllrbmempool, enumbtreemempool}
)

func newMempoolWithAppAndConfig(cc proxy.ClientCreator, config *cfg.Config, me mpEnum) (Mempool, cleanupFunc) {
	appConnMem, _ := cc.NewABCIClient()
	appConnMem.SetLogger(log.TestingLogger().With("module", "abci-client", "connection", "mempool"))
	err := appConnMem.Start()
	if err != nil {
		panic(err)
	}
	legacyProxyAppConnMem := proxy.NewAppConnMempool(appConnMem)
	mempool := mempoolFactory[me](config.Mempool, legacyProxyAppConnMem, 0, false)
	mempool.SetLogger(log.TestingLogger())
	return mempool, func() { os.RemoveAll(config.RootDir) }
}

func newLegacyMempoolWithAppAndConfig(cc proxy.LegacyClientCreator, cfg *cfg.Config, me mpEnum, supIter bool) (Mempool, cleanupFunc) {
	appConnMem, _ := cc.NewABCIClient()
	appConnMem.SetLogger(log.TestingLogger().With("module", "abci-client", "connection", "mempool"))
	err := appConnMem.Start()
	if err != nil {
		panic(err)
	}
	legacyProxyAppConnMem := proxy.NewLegacyAppConnMempool(appConnMem)
	mempool := mempoolFactory[me](cfg.Mempool, proxy.AdaptLegacy(legacyProxyAppConnMem), 0, supIter)
	mempool.SetLogger(log.TestingLogger())
	return mempool, func() { os.RemoveAll(cfg.RootDir) }
}

func ensureNoFire(t *testing.T, ch <-chan struct{}, timeoutMS int) {
	timer := time.NewTimer(time.Duration(timeoutMS) * time.Millisecond)
	select {
	case <-ch:
		t.Fatal("Expected not to fire")
	case <-timer.C:
	}
}

func ensureFire(t *testing.T, ch <-chan struct{}, timeoutMS int) {
	timer := time.NewTimer(time.Duration(timeoutMS) * time.Millisecond)
	select {
	case <-ch:
	case <-timer.C:
		t.Fatal("Expected to fire")
	}
}

func checkTxs(t *testing.T, mempool Mempool, peerID uint16, priorityList []uint64, start int) types.Txs {
	count := len(priorityList)
	txs := make(types.Txs, count)
	txInfo := TxInfo{SenderID: peerID}
	for i := start; i < start+count; i++ {
		txBytes := make([]byte, 20)
		priority := strconv.FormatInt(int64(priorityList[i-start])%100, 10)
		tx := "k" + strconv.Itoa(i) + "=v" + strconv.Itoa(i) + ","
		extra := 20 - len(tx) - len(priority) - 1 // use extra to fill up [20]byte
		// sample tx: k1=v1,ffff..ff,42
		tx = tx + strings.Repeat("f", extra) + "," + priority
		copy(txBytes, tx)
		txs[i-start] = txBytes
		if err := mempool.CheckTx(txBytes, nil, txInfo); err != nil {
			// Skip invalid txs.
			// TestMempoolFilters will fail otherwise. It asserts a number of txs
			// returned.
			if IsPreCheckError(err) {
				continue
			}
			t.Fatalf("CheckTx failed: %v while checking #%d tx", err, i)
		}
	}
	return txs
}

func TestMempoolFilters(t *testing.T) {
	app := kvstore.NewApplication()
	cc := proxy.NewLocalClientCreator(app)

	emptyTxArr := []types.Tx{[]byte{}}

	nopPreFilter := func(tx types.Tx) error { return nil }
	nopPostFilter := func(tx types.Tx, res *abcix.ResponseCheckTx) error { return nil }

	// each table driven test creates numTxsToCreate txs with checkTx, and at the end clears all remaining txs.
	// each tx has 20 bytes
	tests := []struct {
		numTxsToCreate int
		preFilter      PreCheckFunc
		postFilter     PostCheckFunc
		expectedNumTxs int
	}{
		{10, nopPreFilter, nopPostFilter, 10},
		{10, PreCheckMaxBytes(10), nopPostFilter, 0},
		{10, PreCheckMaxBytes(20), nopPostFilter, 10},
		{10, nopPreFilter, PostCheckMaxGas(-1), 10},
		{10, nopPreFilter, PostCheckMaxGas(0), 0},
		{10, nopPreFilter, PostCheckMaxGas(1), 10},
		{10, nopPreFilter, PostCheckMaxGas(3000), 10},
		{10, PreCheckMaxBytes(10), PostCheckMaxGas(20), 0},
		{10, PreCheckMaxBytes(30), PostCheckMaxGas(20), 10},
		{10, PreCheckMaxBytes(20), PostCheckMaxGas(1), 10},
		{10, PreCheckMaxBytes(20), PostCheckMaxGas(0), 0},
	}
	for _, mpEnum := range mpEnums {
		mp, cleanup := newMempoolWithAppAndConfig(cc, cfg.ResetTestRoot(fmt.Sprintf("mempool_test_%d", mpEnum)), mpEnum)
		defer cleanup()
		for tcIndex, tt := range tests {
			err := mp.Update(1, emptyTxArr, abciResponses(len(emptyTxArr), abci.CodeTypeOK), tt.preFilter, tt.postFilter)
			require.NoError(t, err)
			checkTxs(t, mp, UnknownPeerID, make([]uint64, tt.numTxsToCreate), 0)
			require.Equal(t, tt.expectedNumTxs, mp.Size(), "mempool had the incorrect size, on test case %d", tcIndex)
			mp.Flush()
		}
	}
}

func TestMempoolUpdate(t *testing.T) {
	app := kvstore.NewApplication()
	cc := proxy.NewLocalClientCreator(app)

	for _, mpEnum := range mpEnums {
		mp, cleanup := newMempoolWithAppAndConfig(cc, cfg.ResetTestRoot(fmt.Sprintf("mempool_test_%d", mpEnum)), mpEnum)
		defer cleanup()
		// 1. Adds valid txs to the cache
		{
			err := mp.Update(1, []types.Tx{[]byte{0x01}}, abciResponses(1, abci.CodeTypeOK), nil, nil)
			assert.NoError(t, err)
			err = mp.CheckTx([]byte{0x01}, nil, TxInfo{})
			if assert.Error(t, err) {
				assert.Equal(t, ErrTxInCache, err)
			}
		}

		// 2. Removes valid txs from the mempool
		{
			err := mp.CheckTx([]byte{0x02}, nil, TxInfo{})
			require.NoError(t, err)
			err = mp.Update(1, []types.Tx{[]byte{0x02}}, abciResponses(1, abci.CodeTypeOK), nil, nil)
			assert.NoError(t, err)
			assert.Zero(t, mp.Size())
		}

		// 3. Removes invalid transactions from the cache and the mempool (if present)
		{
			err := mp.CheckTx([]byte{0x03}, nil, TxInfo{})
			require.NoError(t, err)
			err = mp.Update(1, []types.Tx{[]byte{0x03}}, abciResponses(1, 1), nil, nil)
			assert.NoError(t, err)
			assert.Zero(t, mp.Size())

			err = mp.CheckTx([]byte{0x03}, nil, TxInfo{})
			assert.NoError(t, err)
		}
	}
}

func TestTxsAvailable(t *testing.T) {
	app := kvstore.NewApplication()
	cc := proxy.NewLocalClientCreator(app)

	for _, mpEnum := range mpEnums {
		mp, cleanup := newMempoolWithAppAndConfig(cc, cfg.ResetTestRoot(fmt.Sprintf("mempool_test_%d", mpEnum)), mpEnum)
		defer cleanup()
		mp.EnableTxsAvailable()

		timeoutMS := 500

		// with no txs, it shouldnt fire
		ensureNoFire(t, mp.TxsAvailable(), timeoutMS)

		// send a bunch of txs, it should only fire once
		txs := checkTxs(t, mp, UnknownPeerID, make([]uint64, 100), 0)
		ensureFire(t, mp.TxsAvailable(), timeoutMS)
		ensureNoFire(t, mp.TxsAvailable(), timeoutMS)

		// call update with half the txs.
		// it should fire once now for the new height
		// since there are still txs left
		committedTxs, txs := txs[:50], txs[50:]
		if err := mp.Update(1, committedTxs, abciResponses(len(committedTxs), abci.CodeTypeOK), nil, nil); err != nil {
			t.Error(err)
		}
		ensureFire(t, mp.TxsAvailable(), timeoutMS)
		ensureNoFire(t, mp.TxsAvailable(), timeoutMS)

		// send a bunch more txs. we already fired for this height so it shouldn't fire again
		moreTxs := checkTxs(t, mp, UnknownPeerID, make([]uint64, 50), 100)
		ensureNoFire(t, mp.TxsAvailable(), timeoutMS)

		// now call update with all the txs. it should not fire as there are no txs left
		committedTxs = append(txs, moreTxs...) //nolint: gocritic
		if err := mp.Update(2, committedTxs, abciResponses(len(committedTxs), abci.CodeTypeOK), nil, nil); err != nil {
			t.Error(err)
		}
		ensureNoFire(t, mp.TxsAvailable(), timeoutMS)

		// send a bunch more txs, it should only fire once
		checkTxs(t, mp, UnknownPeerID, make([]uint64, 100), 150)
		ensureFire(t, mp.TxsAvailable(), timeoutMS)
		ensureNoFire(t, mp.TxsAvailable(), timeoutMS)
	}
}

func TestMempoolCloseWAL(t *testing.T) {
	app := kvstore2.NewApplication()
	cc := proxy.NewLegacyLocalClientCreator(app)
	for _, mpEnum := range mpEnums {
		// 1. Create the temporary directory for mempool and WAL testing.
		rootDir, err := ioutil.TempDir("", fmt.Sprintf("mempool-test-%d", mpEnum))
		require.Nil(t, err, "expecting successful tmpdir creation")

		// 2. Ensure that it doesn't contain any elements -- Sanity check
		m1, err := filepath.Glob(filepath.Join(rootDir, "*"))
		require.Nil(t, err, "successful globbing expected")
		require.Equal(t, 0, len(m1), "no matches yet")

		// 3. Create the mempool
		wcfg := cfg.DefaultConfig()
		wcfg.Mempool.RootDir = rootDir
		mp, cleanup := newLegacyMempoolWithAppAndConfig(cc, wcfg, mpEnum, false)
		defer cleanup()

		mp.(*basemempool).height = 10
		mp.InitWAL()

		// 4. Ensure that the directory contains the WAL file
		m2, err := filepath.Glob(filepath.Join(rootDir, "*"))
		require.Nil(t, err, "successful globbing expected")
		require.Equal(t, 1, len(m2), "expecting the wal match in")

		// 5. Write some contents to the WAL
		mp.CheckTx(types.Tx([]byte("foo")), nil, TxInfo{})
		walFilepath := mp.(*basemempool).wal.Path
		sum1 := checksumFile(walFilepath, t)

		// 6. Sanity check to ensure that the written TX matches the expectation.
		require.Equal(t, sum1, checksumIt([]byte("foo\n")), "foo with a newline should be written")

		// 7. Invoke CloseWAL() and ensure it discards the
		// WAL thus any other write won't go through.
		mp.CloseWAL()
		mp.CheckTx(types.Tx([]byte("bar")), nil, TxInfo{})
		sum2 := checksumFile(walFilepath, t)
		require.Equal(t, sum1, sum2, "expected no change to the WAL after invoking CloseWAL() since it was discarded")

		// 8. Sanity check to ensure that the WAL file still exists
		m3, err := filepath.Glob(filepath.Join(rootDir, "*"))
		require.Nil(t, err, "successful globbing expected")
		require.Equal(t, 1, len(m3), "expecting the wal match in")
	}
}

func TestMempoolMaxMsgSize(t *testing.T) {
	app := kvstore2.NewApplication()
	cc := proxy.NewLegacyLocalClientCreator(app)

	maxTxSize := cfg.TestMempoolConfig().MaxTxBytes
	maxMsgSize := calcMaxMsgSize(maxTxSize)

	testCases := []struct {
		len int
		err bool
	}{
		// check small txs. no error
		{10, false},
		{1000, false},
		{1000000, false},

		// check around maxTxSize
		// changes from no error to error
		{maxTxSize - 2, false},
		{maxTxSize - 1, false},
		{maxTxSize, false},
		{maxTxSize + 1, true},
		{maxTxSize + 2, true},

		// check around maxMsgSize. all error
		{maxMsgSize - 1, true},
		{maxMsgSize, true},
		{maxMsgSize + 1, true},
	}
	for _, mpEnum := range mpEnums {
		mp, cleanup := newLegacyMempoolWithAppAndConfig(cc, cfg.ResetTestRoot(fmt.Sprintf("mempool_test_%d", mpEnum)), mpEnum, false)
		defer cleanup()

		for i, testCase := range testCases {
			caseString := fmt.Sprintf("case %d, len %d", i, testCase.len)

			tx := tmrand.Bytes(testCase.len)
			err := mp.CheckTx(tx, nil, TxInfo{})
			bv := gogotypes.BytesValue{Value: tx}
			bz, err2 := bv.Marshal()
			require.NoError(t, err2)
			require.Equal(t, len(bz), proto.Size(&bv), caseString)
			if !testCase.err {
				require.True(t, len(bz) <= maxMsgSize, caseString)
				require.NoError(t, err, caseString)
			} else {
				require.True(t, len(bz) > maxMsgSize, caseString)
				require.Equal(t, err, ErrTxTooLarge{maxTxSize, testCase.len}, caseString)
			}
		}
	}
}

func TestMempoolTxsBytes(t *testing.T) {
	for _, mpEnum := range mpEnums {
		app := kvstore2.NewApplication()
		cc := proxy.NewLegacyLocalClientCreator(app)
		config := cfg.ResetTestRoot(fmt.Sprintf("mempool_test_%d", mpEnum))
		config.Mempool.MaxTxsBytes = 10
		mempool, cleanup := newLegacyMempoolWithAppAndConfig(cc, config, mpEnum, false)
		defer cleanup()

		// 1. zero by default
		assert.EqualValues(t, 0, mempool.TxsBytes())

		// 2. len(tx) after CheckTx
		err := mempool.CheckTx([]byte{0x01}, nil, TxInfo{})
		require.NoError(t, err)
		assert.EqualValues(t, 1, mempool.TxsBytes())

		// 3. zero again after tx is removed by Update
		mempool.Update(1, []types.Tx{[]byte{0x01}}, abciResponses(1, abci.CodeTypeOK), nil, nil)
		assert.EqualValues(t, 0, mempool.TxsBytes())

		// 4. zero after Flush
		err = mempool.CheckTx([]byte{0x02, 0x03}, nil, TxInfo{})
		require.NoError(t, err)
		assert.EqualValues(t, 2, mempool.TxsBytes())

		mempool.Flush()
		assert.EqualValues(t, 0, mempool.TxsBytes())

		// 5. ErrMempoolIsFull is returned when/if MaxTxsBytes limit is reached.
		err = mempool.CheckTx([]byte{0x04, 0x04, 0x04, 0x04, 0x04, 0x04, 0x04, 0x04, 0x04, 0x04}, nil, TxInfo{})
		require.NoError(t, err)
		err = mempool.CheckTx([]byte{0x05}, nil, TxInfo{})
		if assert.Error(t, err) {
			assert.IsType(t, ErrMempoolIsFull{}, err)
		}

		// 6. zero after tx is rechecked and removed due to not being valid anymore
		app2 := counter.NewApplication(true)
		cc = proxy.NewLegacyLocalClientCreator(app2)
		config = cfg.ResetTestRoot(fmt.Sprintf("mempool_test_%d", mpEnum))
		mempool, cleanup = newLegacyMempoolWithAppAndConfig(cc, config, mpEnum, false)
		defer cleanup()

		txBytes := make([]byte, 8)
		binary.BigEndian.PutUint64(txBytes, uint64(0))

		err = mempool.CheckTx(txBytes, nil, TxInfo{})
		require.NoError(t, err)
		assert.EqualValues(t, 8, mempool.TxsBytes())

		appConnCon, _ := cc.NewABCIClient()
		appConnCon.SetLogger(log.TestingLogger().With("module", "abci-client", "connection", "consensus"))
		err = appConnCon.Start()
		require.Nil(t, err)
		defer appConnCon.Stop()
		res, err := appConnCon.DeliverTxSync(abci.RequestDeliverTx{Tx: txBytes})
		require.NoError(t, err)
		require.EqualValues(t, 0, res.Code)
		res2, err := appConnCon.CommitSync()
		require.NoError(t, err)
		require.NotEmpty(t, res2.Data)

		// Pretend like we committed nothing so txBytes gets rechecked and removed.
		mempool.Update(1, []types.Tx{}, abciResponses(0, abci.CodeTypeOK), nil, nil)
		assert.EqualValues(t, 0, mempool.TxsBytes())
	}
}

func TestBaseMempool_GetNextTxBytes(t *testing.T) {
	app := kvstore.NewApplication()
	cc := proxy.NewLocalClientCreator(app)

	testCases := []struct {
		priorities []uint64
		order      []uint64
		hasError   bool
	}{
		// error case by wrong gas/bytes limit
		{
			priorities: []uint64{0, 0, 0, 0, 0},
			hasError:   true,
		},
		// same priority would present as FIFO
		{
			priorities: []uint64{0, 0, 0, 0, 0},
			order:      []uint64{0, 1, 2, 3, 4},
		},
		{
			priorities: []uint64{1, 0, 1, 0, 1},
			order:      []uint64{0, 3, 1, 4, 2},
		},
		{
			priorities: []uint64{1, 2, 3, 4, 5},
			order:      []uint64{4, 3, 2, 1, 0},
		},
		{
			priorities: []uint64{5, 4, 3, 2, 1},
			order:      []uint64{0, 1, 2, 3, 4},
		},
		{
			priorities: []uint64{1, 3, 5, 4, 2},
			order:      []uint64{4, 2, 0, 1, 3},
		},
		{
			priorities: []uint64{math.MaxUint64, math.MaxUint64, math.MaxUint64, 1},
			order:      []uint64{0, 1, 2, 3},
		},
	}
	for _, mpEnum := range mpEnums {
		mp, cleanup := newMempoolWithAppAndConfig(cc, cfg.ResetTestRoot(fmt.Sprintf("mempool_test_%d", mpEnum)), mpEnum)
		defer cleanup()

		for i, testCase := range testCases {
			originalTxs := checkTxs(t, mp, UnknownPeerID, testCase.priorities, 0)
			remainBytes := 20
			if testCase.hasError {
				remainBytes = 10
			}
			orderedTxs := getTxsWithPriority(mp, int64(remainBytes))
			if testCase.hasError {
				require.Nil(t, orderedTxs, "Failed at testcase %d", i)
				mp.Flush()
				continue
			}
			for j, k := range testCase.order {
				require.Equal(t, originalTxs[j], orderedTxs[k], "Failed at testcase %d", i)
			}
			mp.Flush()
		}
	}
}

func TestCListMempool_RemoveTxs(t *testing.T) {
	app := kvstore2.NewApplication()
	cc := proxy.NewLegacyLocalClientCreator(app)
	for _, mpEnum := range mpEnums {
		config := cfg.ResetTestRoot(fmt.Sprintf("mempool_test_%d", mpEnum))
		mempool, cleanup := newLegacyMempoolWithAppAndConfig(cc, config, mpEnum, false)
		defer cleanup()

		txs := []types.Tx{[]byte{0x01}, []byte{0x02}}
		mempool.CheckTx(txs[0], nil, TxInfo{})
		mempool.CheckTx(txs[1], nil, TxInfo{})
		assert.EqualValues(t, 2, mempool.TxsBytes())

		for j := range txs {
			err := mempool.RemoveTxs(txs[j : j+1])
			require.NoError(t, err)
			assert.EqualValues(t, 1-j, mempool.TxsBytes())
		}
		err := mempool.RemoveTxs(txs[1:])
		require.Error(t, err)
	}
}

func getTxsWithPriority(mempool Mempool, remainBytes int64) []types.Tx {
	var txs []types.Tx
	var next []byte
	for {
		next, _ = mempool.GetNextTxBytes(remainBytes, 1, next)
		if len(next) == 0 {
			break
		}
		txs = append(txs, next)
	}
	return txs
}

func checksumIt(data []byte) string {
	h := sha256.New()
	h.Write(data)
	return fmt.Sprintf("%x", h.Sum(nil))
}

func checksumFile(p string, t *testing.T) string {
	data, err := ioutil.ReadFile(p)
	require.Nil(t, err, "expecting successful read of %q", p)
	return checksumIt(data)
}

func abciResponses(n int, code uint32) []*abcix.ResponseDeliverTx {
	responses := make([]*abcix.ResponseDeliverTx, 0, n)
	for i := 0; i < n; i++ {
		responses = append(responses, &abcix.ResponseDeliverTx{Code: code})
	}
	return responses
}
