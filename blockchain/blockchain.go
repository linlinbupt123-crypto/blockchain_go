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
	"runtime"

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


type BlockChainIterator struct{
	CurrentHash []byte
	Database *badger.DB
}

func DBexists() bool {
	if _, err := os.Stat(dbFile); os.IsNotExist(err){
		return false
	}
	return true
}
func (iter *BlockChainIterator) Next() *Block{
	var block *Block
	err := iter.Database.View(func(txn *badger.Txn) error {
		item, err := txn.Get(iter.CurrentHash)
		if err != nil{
			return err
		}
		var encodedBlock []byte
		err = item.Value(func(val []byte) error {
			encodedBlock = append(encodedBlock, val...)
			return nil
		})
		block = Deserialize(encodedBlock)
		return err
	})
	if err != nil {
		log.Panic(err)
	}
	iter.CurrentHash = block.PrevHash
	return block

}

func (chain *BlockChain) Iterator() *BlockChainIterator{
	iter := &BlockChainIterator{
		chain.LastHash, chain.Database,
	}
	return iter
}

func (chain *BlockChain) AddBlock(transactions []*Transaction) *Block {
	var lastHash []byte

	for _, tx := range transactions {
		if !chain.VerifyTransaction(tx)  {
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
		 return err
	})
	common.HandlerError(err)

	newBlock := CreateBlock(transactions, lastHash)

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




// InitBlockChain, 创建全新区块链
func InitBlockChain(address string) *BlockChain{
var lastHash []byte
if DBexists(){
	fmt.Println("Blockchain already exists")
	runtime.Goexit()
}
	opts := badger.DefaultOptions(dbPath)
	db, err := badger.Open(opts)

	common.HandlerError(err)
	err = db.Update(func(txn *badger.Txn) error {
		cbtx := CoinbaseTx(address, genesisData)
	    genesis := Genesis(cbtx)
		fmt.Println("Genesis created")
		err = txn.Set(genesis.Hash, genesis.Serialize())
		common.HandlerError(err)
		err := txn.Set([]byte("lh"), genesis.Hash)
		common.HandlerError(err)
		lastHash = genesis.Hash
		return nil
	})
	blockchain := BlockChain{lastHash, db}
		return &blockchain
}

// 打开已有区块链
func ContinueBlockChain(address string) *BlockChain{
	if  !DBexists(){
		fmt.Println("No existing blockchain found, create one!")
		runtime.Goexit()
	}
	var lastHash []byte
	opts := badger.DefaultOptions(dbPath)
	db, err := badger.Open(opts)
	common.HandlerError(err)
	err = db.Update(func(txn *badger.Txn) error {
		item, err := txn.Get([]byte("lh"))
		common.HandlerError(err)
		err = item.Value(func(val []byte) error {
			lastHash = append(lastHash, val...)
			return nil
		})
		return err
	})
	common.HandlerError(err)
	return  &BlockChain{lastHash, db}
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

func (bc *BlockChain) SignTransaction(tx *Transaction, privKey *ecdsa.PrivateKey) {
	prevTXs := make(map[string]Transaction)

	for _, in := range tx.Inputs {
		prevTX, err := bc.FindTransaction(in.ID)
		common.HandlerError(err)
		prevTXs[hex.EncodeToString(prevTX.ID)] = prevTX
	}

	tx.Sign(privKey, prevTXs)
}

func (chain *BlockChain) FindUnspentTransactions(pubKeyHash []byte) []Transaction{
	var unspentTxs []Transaction
	spentTXOs := make(map[string][]int)
	iter := chain.Iterator()
	for {
		block := iter.Next()
		for _, tx := range block.Transactions {
			txID := hex.EncodeToString(tx.ID)
			Outputs:
			for outIdx, out := range tx.Outputs{
				if spentTXOs[txID] != nil {
					for _, spentOut := range spentTXOs[txID]{
						if spentOut == outIdx{
							continue Outputs
						}
					}
				}
				if out.IsLockedWithKey(pubKeyHash) {
					unspentTxs = append(unspentTxs, *tx)
				}
			}
			if !tx.IsCoinBase() {
				for _, in := range tx.Inputs{
					if in.UsesKey((pubKeyHash)){
						inTxID := hex.EncodeToString(in.ID)
						spentTXOs[inTxID] = append(spentTXOs[inTxID], in.Out)
					}
				}
			}
		}
		if len(block.PrevHash) == 0{
			break
		}
	}
	return unspentTxs
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
	if tx.IsCoinBase() {
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