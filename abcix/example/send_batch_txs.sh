#!/bin/bash

set -u;

for i in {0..20}; do
	if [ $i -lt 3 ]; then
		curl -s 'localhost:26657/broadcast_tx_commit?tx="xunan'$i'"' &
	elif [ $i -lt 6 ]; then
		curl -s 'localhost:26657/broadcast_tx_commit?tx="xunan=xunan'$i'"' &
	elif [ $i -lt 9 ]; then
		curl -s 'localhost:26657/broadcast_tx_commit?tx="hanyun=hanyun'$i'"' &
	elif [ $i -lt 12 ]; then
		curl -s 'localhost:26657/broadcast_tx_commit?tx="lingyun=lingyun'$i'"' &
	else
		curl -s 'localhost:26657/broadcast_tx_commit?tx="junjia=junjia'$i'"' &
	fi
done

