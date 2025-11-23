package blockchain

/*
比特币区块链的存储需求很大，可能达到200GB。
为了验证交易，最关键的数据是未花费的交易输出（UTXO），通过索引这些数据，可以减少存储需求。
这种优化技术可能是指某些具体的实现或方法，能够在不存储整个区块链的情况下高效地验证交易。
*/
/*
Badger 数据库来存储和管理 未花费交易输出（UTXO）集合
UTXO Set 是一个集合，它包含了所有未花费的交易输出。在比特币等区块链中，交易通过使用“未花费的交易输出”（UTXO）来构建。一个有效的交易必须引用之前的交易输出，并消耗它们。
在这个结构中，UTXO Set 包含一个区块链字段，这样就可以访问与区块链相关的数据库。
然后，你可以在数据库中创建一个新的层，这个层专门用来存储 UTXO 数据。
*/
// UTXO Set 用于存储和管理所有未花费的交易输出（UTXO）。
import (
	"blockchain_go/common"
	"bytes"
	"encoding/hex"
	"log"

	"github.com/dgraph-io/badger"
)

var (
	utxoPrefix   = []byte("utxo-")
	// prefixLength = len(utxoPrefix)
)

type UTXOSet struct {
	Blockchain *BlockChain
}

// FindSpendableOutputs
func (u UTXOSet) FindSpendableOutputs(pubKeyHash []byte, amount int) (int, map[string][]int) {
	unspentOuts := make(map[string][]int)
	accumulated := 0
	db := u.Blockchain.Database

	err := db.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions

		it := txn.NewIterator(opts)
		defer it.Close()

		for it.Seek(utxoPrefix); it.ValidForPrefix(utxoPrefix); it.Next() {
			item := it.Item()
			k := item.Key()
			var v []byte
			err := item.Value(func(val []byte) error {
				v = append(v, val...)
				return nil
			})
			common.HandlerError(err)
			k = bytes.TrimPrefix(k, utxoPrefix)
			txID := hex.EncodeToString(k)
			outs := DeserializeOutputs(v)

			for outIdx, out := range outs.Outputs {
				if out.IsLockedWithKey(pubKeyHash) && accumulated < amount {
					accumulated += out.Value
					unspentOuts[txID] = append(unspentOuts[txID], outIdx)
				}
			}
		}
		return nil
	})
	common.HandlerError(err)
	return accumulated, unspentOuts
}

func (u UTXOSet) FindUTXO(pubKeyHash []byte) []TxOutput {
	var UTXOs []TxOutput

	db := u.Blockchain.Database

	err := db.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions

		it := txn.NewIterator(opts)
		defer it.Close()

		for it.Seek(utxoPrefix); it.ValidForPrefix(utxoPrefix); it.Next() {
			item := it.Item()
			var v []byte
			err := item.Value(func(val []byte) error {
				v = append(v, val...)
				return nil
			})
			common.HandlerError(err)
			outs := DeserializeOutputs(v)

			for _, out := range outs.Outputs {
				if out.IsLockedWithKey(pubKeyHash) {
					UTXOs = append(UTXOs, out)
				}
			}
		}

		return nil
	})
	common.HandlerError(err)

	return UTXOs
}

func (u UTXOSet) CountTransactions() int {
	db := u.Blockchain.Database
	counter := 0

	err := db.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions

		it := txn.NewIterator(opts)
		defer it.Close()
		for it.Seek(utxoPrefix); it.ValidForPrefix(utxoPrefix); it.Next() {
			counter++
		}

		return nil
	})

	common.HandlerError(err)

	return counter
}
/* 
 Reindex , 这段代码定义了 Reindex() 函数，这个函数是 UTXOSet 的一部分，目的是重建并重新索引 UTXO（未花费交易输出）。
 在比特币等区块链中，UTXO 是有效交易的一部分，表示某一交易的输出，可以被新的交易使用。
 在这段代码中，Reindex() 函数通过删除旧的 UTXO 数据并将新的 UTXO 插入到数据库来重新索引这些数据。
*/
// Reindex() 1,清空数据库中旧的 UTXO 数据（使用 DeleteByPrefix 删除）。2, 从区块链中获取最新的 UTXO 数据（通过 FindUTXO）。3, 遍历这些 UTXO 数据并将它们插入到数据库，通过在每个键前加上前缀来确保数据的组织结构。
func (u UTXOSet) Reindex() {
	db := u.Blockchain.Database
    // 这个方法调用的目的是删除数据库中所有以 utxoPrefix 开头的键（即所有与当前 UTXO 相关的数据）。这一步是清空数据库中的现有 UTXO 数据，为插入新数据做准备。
	u.DeleteByPrefix(utxoPrefix)
    // 这里通过调用 u.Blockchain.FindUTXO() 来获取当前区块链中的所有 UTXO。这个方法返回一个数据结构（例如一个字典或映射），其中键是交易 ID（txId），值是该交易的输出（outs）。
	UTXO := u.Blockchain.FindUTXO()

	err := db.Update(func(txn *badger.Txn) error {
		for txId, outs := range UTXO {
			key, err := hex.DecodeString(txId)
			if err != nil {
				return err
			}
			key = append(utxoPrefix, key...)

			err = txn.Set(key, outs.Serialize())
			common.HandlerError(err)
		}

		return nil
	})
	common.HandlerError(err)
}

func (u *UTXOSet) Update(block *Block) {
	db := u.Blockchain.Database

	err := db.Update(func(txn *badger.Txn) error {
		for _, tx := range block.Transactions {
			if !tx.IsCoinBase() {
				for _, in := range tx.Inputs {
					updatedOuts := TxOutputs{}
					inID := append(utxoPrefix, in.ID...)
					item, err := txn.Get(inID)
					common.HandlerError(err)
					var v []byte
					err = item.Value(func(val []byte) error {
						v = append(v, val...)
						return nil
					})
					common.HandlerError(err)

					outs := DeserializeOutputs(v)

					for outIdx, out := range outs.Outputs {
						if outIdx != in.Out {
							updatedOuts.Outputs = append(updatedOuts.Outputs, out)
						}
					}

					if len(updatedOuts.Outputs) == 0 {
						if err := txn.Delete(inID); err != nil {
							log.Panic(err)
						}

					} else {
						if err := txn.Set(inID, updatedOuts.Serialize()); err != nil {
							log.Panic(err)
						}
					}
				}
			}

			newOutputs := TxOutputs{}
			newOutputs.Outputs = append(newOutputs.Outputs, tx.Outputs...)

			txID := append(utxoPrefix, tx.ID...)
			if err := txn.Set(txID, newOutputs.Serialize()); err != nil {
				log.Panic(err)
			}
		}

		return nil
	})
	common.HandlerError(err)
}

func (u *UTXOSet) DeleteByPrefix(prefix []byte) {
	deleteKeys := func(keysForDelete [][]byte) error {
		if err := u.Blockchain.Database.Update(func(txn *badger.Txn) error {
			for _, key := range keysForDelete {
				if err := txn.Delete(key); err != nil {
					return err
				}
			}
			return nil
		}); err != nil {
			return err
		}
		return nil
	}

	collectSize := 100000

	// 以只读模式重新打开数据库，这意味着只是从数据库中读取数据，而不会进行修改。通过这种方式，可以确保不会不小心修改到数据库中的数据。
    // 你需要传入一个 闭包（closure） 来访问 Badger 的事务对象。这是因为 Badger 的操作是基于事务的，必须在事务的上下文中执行。
	err := u.Blockchain.Database.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		/*
		Prefetch values（预取值）是 Badger 迭代器的一个选项。它允许你在遍历键时，只读取键本身，而不立即获取与这些键相关联的值。
        这种方法可以显著提高性能，尤其是在你只需要遍历键并不需要立即访问每个键对应的值时。
		通过预取键，你可以避免对数据库进行不必要的额外读取，从而节省时间和资源。
		*/
		opts.PrefetchValues = false
		it := txn.NewIterator(opts)
		defer it.Close()

		keysForDelete := make([][]byte, 0, collectSize)
		keysCollected := 0
		for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
			key := it.Item().KeyCopy(nil)
			keysForDelete = append(keysForDelete, key)
			keysCollected++
			if keysCollected == collectSize {
				if err := deleteKeys(keysForDelete); err != nil {
					log.Panic(err)
				}
				keysForDelete = make([][]byte, 0, collectSize)
				keysCollected = 0
			}
		}
		if keysCollected > 0 {
			if err := deleteKeys(keysForDelete); err != nil {
				log.Panic(err)
			}
		}
		return nil
	})
	if err != nil{
		common.HandlerError(err)
	}
}
