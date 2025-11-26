package blockchain

import (
	"log"

	"github.com/dgraph-io/badger"
)

type BlockChainIterator struct{
	CurrentHash []byte
	Database *badger.DB
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