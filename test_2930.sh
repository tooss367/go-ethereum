# Rremove teh genesis
rm -rf /tmp/bax

# Init the genesis
./build/bin/geth --datadir /tmp/bax init ./core/testdata/acl_genesis.json

# Import the blocks
./build/bin/geth --fakepow --datadir /tmp/bax import ./core/testdata/acl_block_0.rlp
./build/bin/geth --fakepow --datadir /tmp/bax import ./core/testdata/acl_block_1.rlp
./build/bin/geth --fakepow --datadir /tmp/bax import ./core/testdata/acl_block_2.rlp
./build/bin/geth --fakepow --datadir /tmp/bax import ./core/testdata/acl_block_3.rlp
./build/bin/geth --fakepow --datadir /tmp/bax import ./core/testdata/acl_block_4.rlp
./build/bin/geth --fakepow --datadir /tmp/bax import ./core/testdata/acl_block_5.rlp
./build/bin/geth --fakepow --datadir /tmp/bax import ./core/testdata/acl_block_6.rlp
./build/bin/geth --fakepow --datadir /tmp/bax import ./core/testdata/acl_block_7.rlp
./build/bin/geth --fakepow --datadir /tmp/bax import ./core/testdata/acl_block_8.rlp
./build/bin/geth --fakepow --datadir /tmp/bax import ./core/testdata/acl_block_9.rlp

# Dump out one of the transactions

./build/bin/geth --datadir /tmp/bax --nodiscover --maxpeers 0 console --exec "b=eth.getBlock(3); debug.traceTransaction(b.transactions[0])"



