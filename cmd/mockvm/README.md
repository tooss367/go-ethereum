## MockVM

MockVM is based off a proof of concept VM that was created for EthBerlin 2018, for Vlad Zamfir / Aurelian 's sharding entry. 

It is a standalone VM  which executes a json-file describing the prestate, and applies a set of transactions in the order they appear.
See `example.json` for specific syntax.

The MockVM contains a bridge to javascript, so an outside js engine can implement raw vm operations. This is useful for example to 
modify the returnvalues from things like `BALANCE` or `BLOCKHASH` etc, gior really any vm operation.

It uses `Constantinople` rules!

OBS: The transactions use the same chain id as mainnet, so be careful (because anything can be replayed across them)

The example file contains two identical transactions, after each other. This is fully legit, and demonstrates that the second one is rejected
since it has an invalid nonce.

## Example

The `evm.js` contains the javascript execution method that override evm execution. 

```javascript
function op_BLOCKHASH(){
	return JSON.stringify(["0x1337"])
}

```

It also contains a method which tells the framework which ops to override:
```javascript

function jsOverrides(){
    return JSON.stringify(["BLOCKHASH"])
}

```

The methods are called with the stack parameters, and are expected to return the 
correct number of return values. 
If the js execution fails, the vm falls back to the 'native' executor. 


When running it, this is the `stderr` output for the example:

```

[user@work mockvm]$ ./mockvm --json --verbosity 1 apply example.json 1>/dev/null
{"pc":0,"op":96,"gas":"0xf0000","gasCost":"0x0","memory":"0x","memSize":0,"stack":[],"depth":1,"refund":0,"opName":"PUSH1","error":""}
{"pc":2,"op":64,"gas":"0xefffd","gasCost":"0x0","memory":"0x","memSize":0,"stack":["0x1"],"depth":1,"refund":0,"opName":"BLOCKHASH","error":""}
{"pc":3,"op":96,"gas":"0xeffe9","gasCost":"0x0","memory":"0x","memSize":0,"stack":["0x1337"],"depth":1,"refund":0,"opName":"PUSH1","error":""}
{"pc":5,"op":0,"gas":"0xeffe6","gasCost":"0x0","memory":"0x","memSize":0,"stack":["0x1337","0x1"],"depth":1,"refund":0,"opName":"STOP","error":""}
{"output":"","gasUsed":"0x1a","time":194764}
rejected tx: 0x01c56ab0ebdf6c542653f9cb15ecbcca82720eeeb014c790ac1907dbd0aa3461 from 0xc1a4af9092110767d6efd4eb56c5091c15f18912: nonce too low
{"stateRoot": "6c7db20bd6cb684dab6e96358bf8d961917a6c2bc5f1d2f8b0ff906fd4f7cc79"}

```

The `"stack":["0x1337"]` after `BLOCKHASH` shows that our js operation worked fine!

You can get more verbose output:
```
[user@work mockvm]$ ./mockvm --json --verbosity 5 apply example.json 1>/dev/null
INFO [03-25|09:50:13.560] js overrides                             overrides=[BLOCKHASH]
INFO [03-25|09:50:13.561] Persisted trie from memory database      nodes=4 size=445.00B time=26.278µs gcnodes=0 gcsize=0.00B gctime=0s livenodes=2 livesize=32.00B
{"pc":0,"op":96,"gas":"0xf0000","gasCost":"0x0","memory":"0x","memSize":0,"stack":[],"depth":1,"refund":0,"opName":"PUSH1","error":""}
{"pc":2,"op":64,"gas":"0xefffd","gasCost":"0x0","memory":"0x","memSize":0,"stack":["0x1"],"depth":1,"refund":0,"opName":"BLOCKHASH","error":""}
INFO [03-25|09:50:13.562] invoking js op                           call="op_BLOCKHASH(\"0x1\")"
{"pc":3,"op":96,"gas":"0xeffe9","gasCost":"0x0","memory":"0x","memSize":0,"stack":["0x1337"],"depth":1,"refund":0,"opName":"PUSH1","error":""}
{"pc":5,"op":0,"gas":"0xeffe6","gasCost":"0x0","memory":"0x","memSize":0,"stack":["0x1337","0x1"],"depth":1,"refund":0,"opName":"STOP","error":""}
{"output":"","gasUsed":"0x1a","time":235495}
rejected tx: 0x01c56ab0ebdf6c542653f9cb15ecbcca82720eeeb014c790ac1907dbd0aa3461 from 0xc1a4af9092110767d6efd4eb56c5091c15f18912: nonce too low
{"stateRoot": "6c7db20bd6cb684dab6e96358bf8d961917a6c2bc5f1d2f8b0ff906fd4f7cc79"}

```

This is the `stdout` output -- the complete post-state: 
```json
{
  "state": {
    "root": "6c7db20bd6cb684dab6e96358bf8d961917a6c2bc5f1d2f8b0ff906fd4f7cc79",
    "accounts": {
      "8a8eafb1cf62bfbeb1741769dae1a9dd47996192": {
        "balance": "4276993710",
        "nonce": 0,
        "root": "56e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421",
        "codeHash": "937f5873aa23d7b0467c4c0f20b3e21749655c84f7046ec0252c63c9ad32ca5e",
        "code": "6001406001",
        "storage": {}
      },
      "c1a4af9092110767d6efd4eb56c5091c15f18912": {
        "balance": "309484998291365281148554065",
        "nonce": 1,
        "root": "56e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421",
        "codeHash": "c5d2460186f7233c927e7db2dcc703c0e500b653ca82273b7bfad8045d85a470",
        "code": "",
        "storage": {}
      },
      "c94f5374fce5edbc8e2a8697c15331677e6ebf0b": {
        "balance": "21026",
        "nonce": 0,
        "root": "56e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421",
        "codeHash": "c5d2460186f7233c927e7db2dcc703c0e500b653ca82273b7bfad8045d85a470",
        "code": "",
        "storage": {}
      }
    }
  },
  "receipts": [
    {
      "root": "0x",
      "status": "0x1",
      "cumulativeGasUsed": "0x5222",
      "logsBloom": "0x00000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000",
      "logs": null,
      "transactionHash": "0x01c56ab0ebdf6c542653f9cb15ecbcca82720eeeb014c790ac1907dbd0aa3461",
      "contractAddress": "0x0000000000000000000000000000000000000000",
      "gasUsed": "0x5222"
    }
  ],
  "rejected": [
    "0x01c56ab0ebdf6c542653f9cb15ecbcca82720eeeb014c790ac1907dbd0aa3461"
  ]
}
```

## Notes 

This is very much work in progress. Some things not implemented (yet) are

- There's no support for setting return data from calls or revert, 
- There's no way to read/write memory contents from javascript, 
- There's no way to know the current address, calldata etc. 


