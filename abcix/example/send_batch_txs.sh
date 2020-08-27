#!/bin/bash

set -eux;

for p in `seq 0 49 | sort -R`; do
		curl -s 'localhost:26657/broadcast_tx_async?tx="key'$p'=val'$p',junjiah,'$p'"' > /dev/null
done

# sample query to get back result from a certain block height
# curl -s localhost:26657/block?height=5 | jq -r .result.block.data.txs[] | xargs -I {} sh -c "echo {} | base64 --decode; echo"
