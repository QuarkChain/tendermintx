#!/bin/bash

set -eux;

for p in `seq 0 49 | sort -R`; do
		curl -s 'localhost:26657/broadcast_tx_async?tx="key'$p'=val'$p',junjiah,'$p'"' > /dev/null
done
