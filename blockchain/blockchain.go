package blockchain

import (
	"blockchain_go/common"
	"bytes"
	"crypto/ecdsa"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/dgraph-io/badger"
)
const (
	dbPath = "./tmp/blocks"
	dbFile = "./tmp/blocks/MANIFEST"
	genesisData = "First Transaction from Genesis"
)

type BlockChain struct{
	LastHash []byte
	Database *badger.DB
}



func DBexists(path string) bool {
	if _, err := os.Stat(path + "/MANIFEST"); os.IsNotExist(err) {
		return false
	}

	return true
}


func (chain *BlockChain) Iterator() *BlockChainIterator{
	iter := &BlockChainIterator{
		chain.LastHash, chain.Database,
	}
	return iter
}

func (chain *BlockChain) AddBlock(block *Block) error {
	err := chain.Database.Update(func(txn *badger.Txn) error {
		var lastHash []byte
		if _, err := txn.Get(block.Hash); err == nil {
			return nil
		}

		blockData := block.Serialize()
		if err := txn.Set(block.Hash, blockData); err != nil{
			return err
		}

		item, err := txn.Get([]byte("lh"))
		if err != nil {
			return err
		}
		if err:= item.Value(func(val []byte) error {
			lastHash = append(lastHash, val...)
			return nil
		}); err != nil{
			return err
		}

		item, err = txn.Get(lastHash)
		if err != nil {
			return err
		}
		var lastBlockData []byte
		if err := item.Value(func(val []byte) error {
			lastBlockData = append(lastBlockData, val...)
			return nil
		}); err != nil{
			return err
		}

		lastBlock := Deserialize(lastBlockData)

		if block.Height > lastBlock.Height {
			if err = txn.Set([]byte("lh"), block.Hash); err != nil{
				return err
			}
			chain.LastHash = block.Hash
		}

		return nil
	})
	return err
}


func (chain *BlockChain) GetBestHeight() (int, error) {
	var lastBlock Block

	err := chain.Database.View(func(txn *badger.Txn) error {
		item, err := txn.Get([]byte("lh"))
		if err != nil{
			return err
		}
		var lastHash []byte
		var lastBlockData []byte
		if err := item.Value(func(val []byte) error {
			lastHash = append(lastHash, val...)
			return nil
		}); err != nil{
			return err
		}

		item, err = txn.Get(lastHash)
		if err != nil{
			return err
		}
		if err:= item.Value(func(val []byte) error {
			lastBlockData = append(lastBlockData, val...)
			return nil
		}); err != nil{
			return err
		}

		lastBlock = *Deserialize(lastBlockData)
		return nil
	})
	if err != nil{
		return 0, err
	}

	return lastBlock.Height, nil
}

// InitBlockChain, 创建全新区块链
func InitBlockChain(address, nodeId string) (*BlockChain, error) {
	path := fmt.Sprintf(dbPath, nodeId)
	if DBexists(path) {
		fmt.Println("Blockchain already exists")
		runtime.Goexit()
	}
	var lastHash []byte
	opts := badger.DefaultOptions(path)

	db, err := openDB(path, opts)
	if err != nil{
		return nil, err
	}

	if err := db.Update(func(txn *badger.Txn) error {
		cbtx := CoinbaseTx(address, genesisData)
		genesis := Genesis(cbtx)
		fmt.Println("Genesis created")
		if err = txn.Set(genesis.Hash, genesis.Serialize()); err != nil {
			return err
		}
		err = txn.Set([]byte("lh"), genesis.Hash)

		lastHash = genesis.Hash

		return err

	}); err != nil{
		return nil, err
	}

	blockchain := BlockChain{lastHash, db}
	return &blockchain, nil
}


// 打开已有区块链
func ContinueBlockChain(nodeId string) (*BlockChain,error) {
	path := fmt.Sprintf(dbPath, nodeId)
	if DBexists(path) == false {
		fmt.Println("No existing blockchain found, create one!")
		runtime.Goexit()
	}

	var lastHash []byte

	opts := badger.DefaultOptions(path)

	db, err := openDB(path, opts)
	if err != nil {
		return nil, err
	}
	

	if err = db.Update(func(txn *badger.Txn) error {
		item, err := txn.Get([]byte("lh"))
		if err != nil{
			return err
		}
		err = item.Value(func(val []byte) error {
			lastHash = append(lastHash, val...)
			return badger.ErrNilCallback
		})

		return err
	}); err != nil{
		return nil, err
	}


	chain := BlockChain{lastHash, db}

	return &chain, nil
}


func (bc *BlockChain) FindTransaction(ID []byte) (Transaction, error) {
	iter := bc.Iterator()

	for {
		block := iter.Next()
		for _, tx := range block.Transactions {
			if bytes.Equal(tx.ID, ID)  {
				return *tx, nil
			}
		}

		if len(block.PrevHash) == 0 {
			break
		}
	}

	return Transaction{}, errors.New("Transaction does not exist")
}

func (chain *BlockChain) GetBlock(blockHash []byte) (Block, error) {
	var block Block

	err := chain.Database.View(func(txn *badger.Txn) error {
		if item, err := txn.Get(blockHash); err != nil {
			return errors.New("Block is not found")
		} else {
			var blockData []byte
			if err := item.Value(func(val []byte) error {
				blockData = append(blockData, val...)
				return nil
			}); err != nil{
				return err
			}

			block = *Deserialize(blockData)
		}
		return nil
	})
	if err != nil {
		return block, err
	}

	return block, nil
}
func (chain *BlockChain) GetBlockHashes() [][]byte {
	var blocks [][]byte

	iter := chain.Iterator()

	for {
		block := iter.Next()

		blocks = append(blocks, block.Hash)

		if len(block.PrevHash) == 0 {
			break
		}
	}

	return blocks
}
func (chain *BlockChain) MineBlock(transactions []*Transaction) *Block {
	var lastHash []byte
	var lastHeight int
	var lastBlockData[]byte
	for _, tx := range transactions {
		if chain.VerifyTransaction(tx) != true {
			log.Panic("Invalid Transaction")
		}
	}

	err := chain.Database.View(func(txn *badger.Txn) error {
		item, err := txn.Get([]byte("lh"))
		common.HandlerError(err)
		err = item.Value(func(val []byte) error {
			lastHash = append(lastHash, val...)
			return nil
		})

		item, err = txn.Get(lastHash)
		common.HandlerError(err)
		
		err = item.Value(func(val []byte) error {
			lastBlockData = append(lastBlockData, val...)
			return nil
		})

		lastBlock := Deserialize(lastBlockData)

		lastHeight = lastBlock.Height

		return err
	})
	common.HandlerError(err)

	newBlock := CreateBlock(transactions, lastHash, lastHeight+1)

	err = chain.Database.Update(func(txn *badger.Txn) error {
		err := txn.Set(newBlock.Hash, newBlock.Serialize())
		common.HandlerError(err)
		err = txn.Set([]byte("lh"), newBlock.Hash)

		chain.LastHash = newBlock.Hash

		return err
	})
	common.HandlerError(err)

	return newBlock
}
func (bc *BlockChain) SignTransaction(tx *Transaction, privKey *ecdsa.PrivateKey) {
	prevTXs := make(map[string]Transaction)

	for _, in := range tx.Inputs {
		prevTX, err := bc.FindTransaction(in.ID)
		common.HandlerError(err)
		prevTXs[hex.EncodeToString(prevTX.ID)] = prevTX
	}

	tx.Sign(privKey, prevTXs)
}

func (chain *BlockChain) FindUTXO() map[string]TxOutputs{
	UTXO := make(map[string]TxOutputs)
	spentTXOs := make(map[string][]int)

	iter := chain.Iterator()

	for {
		block := iter.Next()

		for _, tx := range block.Transactions {
			txID := hex.EncodeToString(tx.ID)

		Outputs:
			for outIdx, out := range tx.Outputs {
				if spentTXOs[txID] != nil {
					for _, spentOut := range spentTXOs[txID] {
						if spentOut == outIdx {
							continue Outputs
						}
					}
				}
				outs := UTXO[txID]
				outs.Outputs = append(outs.Outputs, out)
				UTXO[txID] = outs
			}
			if !tx.IsCoinBase() {
				for _, in := range tx.Inputs {
					inTxID := hex.EncodeToString(in.ID)
					spentTXOs[inTxID] = append(spentTXOs[inTxID], in.Out)
				}
			}
		}

		if len(block.PrevHash) == 0 {
			break
		}
	}
	return UTXO
}

func (bc *BlockChain) VerifyTransaction(tx *Transaction) bool {
	if tx.IsCoinbase() {
		return true
	}
	prevTXs := make(map[string]Transaction)

	for _, in := range tx.Inputs {
		prevTX, err := bc.FindTransaction(in.ID)
		common.HandlerError(err)
		prevTXs[hex.EncodeToString(prevTX.ID)] = prevTX
	}

	return tx.Verify(prevTXs)
}

func retry(dir string, originalOpts badger.Options) (*badger.DB, error) {
	lockPath := filepath.Join(dir, "LOCK")
	if err := os.Remove(lockPath); err != nil {
		return nil, fmt.Errorf(`removing "LOCK": %s`, err)
	}
	retryOpts := originalOpts
	retryOpts.Truncate = true
	db, err := badger.Open(retryOpts)
	return db, err
}

func openDB(dir string, opts badger.Options) (*badger.DB, error) {
	if db, err := badger.Open(opts); err != nil {
		if strings.Contains(err.Error(), "LOCK") {
			if db, err := retry(dir, opts); err == nil {
				log.Println("database unlocked, value log truncated")
				return db, nil
			}
			log.Println("could not unlock database:", err)
		}
		return nil, err
	} else {
		return db, nil
	}
}