#!/bin/bash

set -u;

for i in {0..20}; do
	if [ "$i" = "0" ]; then
			curl -s 'localhost:26657/broadcast_tx_commit?tx="xunan"' &
	else
			curl -s 'localhost:26657/broadcast_tx_commit?tx="hanyun'$i'"' &
	fi
done

