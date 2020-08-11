package state

import (
	abcix "github.com/tendermint/tendermint/abcix/types"
	"github.com/tendermint/tendermint/abci/types"
	cfg "github.com/tendermint/tendermint/config"

	"github.com/jinzhu/copier"
)

var config = cfg.DefaultBaseConfig()

func (m *ABCIResponses) ValidatorUpdates() types.ValidatorUpdates {
	if m.EndBlock != nil {
		return m.EndBlock.ValidatorUpdates
	}
	var deliverBlockValidatorUpdates types.ValidatorUpdates
	for i, v := range m.DeliverBlock.ValidatorUpdates {
		copier.Copy(&deliverBlockValidatorUpdates[i], &v)
	}
	return deliverBlockValidatorUpdates
}

func (m *ABCIResponses) ConsensusParamUpdates() *types.ConsensusParams {
	if m.EndBlock != nil {
		return m.EndBlock.ConsensusParamUpdates
	}

	var deliverBlockConsensusParamUpdates = types.ConsensusParams{}
	copier.Copy(&(deliverBlockConsensusParamUpdates.Evidence), m.DeliverBlock.ConsensusParamUpdates.Evidence)
	copier.Copy(&(deliverBlockConsensusParamUpdates.Block), m.DeliverBlock.ConsensusParamUpdates.Block)
	copier.Copy(&(deliverBlockConsensusParamUpdates.Validator), m.DeliverBlock.ConsensusParamUpdates.Validator)
	copier.Copy(&(deliverBlockConsensusParamUpdates.Version), m.DeliverBlock.ConsensusParamUpdates.Version)

	return &deliverBlockConsensusParamUpdates
}

func (m *ABCIResponses) UpdateDeliverTx(dtxs []*types.ResponseDeliverTx) {
	if config.DeliverBlock {
		if m.DeliverBlock == nil {
			m.DeliverBlock = new(abcix.ResponseDeliverBlock)
			m.DeliverBlock.DeliverTxs = make([]*abcix.ResponseDeliverTx, len(dtxs))
		}
		for i, tx := range dtxs {
			copier.Copy(&(m.DeliverBlock.DeliverTxs[i]), tx)
		}

	} else {
		m.DeliverTxs = dtxs
	}
}

func (m *ABCIResponses) UpdateDeliverTxByIndex(txRes *types.ResponseDeliverTx, idx int) {
	if config.DeliverBlock {
		copier.Copy(&(m.DeliverBlock.DeliverTxs[idx]), txRes)
	} else {
		m.DeliverTxs[idx] = txRes
	}
}
