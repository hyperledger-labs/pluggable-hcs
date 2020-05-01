## Build Your First Network (BYFN) with HCS-Based Ordering Service

This is a simple guide on how to run the first network sample with HCS-based ordering service. The steps
in this guide have been validated on Mac OSX (Catalina) and Ubuntu 18.04 LTS.

### Prerequisites

1. Toolchain: git, make, gcc, go, docker, and docker-compose. Please refer to [Prerequsites for Hyperledger Fabric v2.0](https://hyperledger-fabric.readthedocs.io/en/release-2.0/prereqs.html)
for the complete list with detailed instructions to set up the build environment for Hyperledger Fabric.
1. The hyperledger fabric repo with HCS orderer PoC code and the first-network sample. Please follow the instructions below to get the source, and hereafter
the repo directory will be referred as FABRIC_REPO_DIR.
   ```
   $ mkdir -p $GOPATH/src/github.com/hyperledger && cd $GOPATH/src/github.com/hyperledger
   $ git clone https://github.com/hashgraph/fabric-hcs.git fabric
   $ cd fabric
   $ git checkout hcs-dev
   ```
1. A hedera testnet account. Please go to [Hedera Portal](https://portal.hedera.com) to register an account if you don't have one.
Please note down your account id and private key.

### Build Fabric Binaries and Docker Images

Follow the instructions in this section to build the required fabric binaries and docker images.

```
$ cd $FABRIC_REPO_DIR
$ make clean
$ make configtxgen configtxlator cryptogen orderer peer docker
```

### Configuration

#### $FABRIC_REPO_DIR/first-network/hedera_env.json

Edit ```hedera_env.json``` and fill in the values for ```operatorId``` and ```operatorKey``` with your hedera testnet
account id and private key.

#### $FABRIC_REPO_DIR/first-network/orderer.yaml

Edit ```orderer.yaml``` and fill in the values for ```Hcs.Operator.Id``` and ```Hcs.Operator.PrivateKey.Key``` with your hedera
testnet acccount id and private key.

### Run the BYFN Sample

Run the sample:

```
$ cd $FABRIC_REPO_DIR/first-network
$ ./byfn.sh up -t 20
```

Please note it's necessary to set the timeout to 20 seconds since otherwise some checks in the script may fail due to the nature
that HCS has a larger consensus delay than raft/kafka based ordering service.

The sample is almost identical with the upstream first-network sample as of 2.0.0 release. In summary, the modifications are:

1. Updated docker compose files to use hcs-based orderers.
2. Added a function in ```byfn.sh``` to generate a 256-bit AES key for data encryption/decryption between fabric orderers and hedera testnet.
3. Added a function in ```byfn.sh``` to use the hcscli tool for HCS topic creation and mapping topics to fabric channels.

A successful run will end with the following message:

```
========= All GOOD, BYFN execution completed ===========


 _____   _   _   ____
| ____| | \ | | |  _ \
|  _|   |  \| | | | | |
| |___  | |\  | | |_| |
|_____| |_| \_| |____/
```

To tear down the environment and clean up intermediate files,

```
$ ./byfn.sh down
```

Optional cleanup of HCS topics after each run. The ```byfn.sh``` script automatically creates two HCS topics. You can find the topic IDs at the beginning of
the console output,

```
generated HCS topics: 0.0.179950 0.0.179951
```

then run the command below with the topic IDs to delete them,

```
$ ../build/bin/hcscli topic delete 0.0.179950 0.0.179951
```
