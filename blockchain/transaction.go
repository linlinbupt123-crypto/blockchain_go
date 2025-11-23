package blockchain

import (
	"blockchain_go/common"
	"blockchain_go/wallet"
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/gob"
	"encoding/hex"
	"fmt"
	"log"
	"math/big"
	"strings"
)

/*
在 UTXO 模型下，每笔交易的 输入（Input） 都会引用上一笔交易的 输出（Output）。
“unlock an output” 指的是：验证我们是否有权花掉某个输出。
也就是说，你要花一笔钱（创建新的输入），你必须证明你“拥有”之前的输出，比如通过数字签名。
*/

// Transaction, 用哪些钱（Inputs）支付给谁（Outputs）
type Transaction struct{
	ID []byte
	Inputs []TxInput   // 这笔交易花掉哪些 UTXO
	Outputs []TxOutput //产生新的 UTXO（给谁、多少钱）
}
func (tx *Transaction) Hash() []byte {
	var hash [32]byte

	txCopy := *tx
	txCopy.ID = []byte{}

	hash = sha256.Sum256(txCopy.Serialize())

	return hash[:]
}
/*
A transaction is a coinbase transaction.
Coinbase transaction 是比特币网络里挖矿奖励交易。
特点：它没有真正引用前一笔交易的输出（UTXO），它直接生成新币。

所以要判断某笔交易是不是 coinbase，需要特定的规则。
这些函数的作用是判断交易类型，例如普通转账还是挖矿奖励（coinbase transaction）。
*/
// IsCoinBase, A transaction is a coinbase transaction. 挖矿奖励交易
func (tx *Transaction) IsCoinBase() bool{
	// Coinbase 交易 只有一个输入（Input）: len(tx.Inputs) == 1
	// Coinbase 的唯一输入 没有引用前一笔交易，它的 TxID 是 0。这个输入是“空引用”，不是指向任何真实交易。需要判断tx.Inputs[0].Txid == []byte{}: len(tx.Inputs[0].ID) 
	// Coinbase 输入的输出索引 设为 -1（表示没有真正引用前一个输出）。:tx.Inputs[0].Out == -1
	return len(tx.Inputs) == 1 && len(tx.Inputs[0].ID) == 0 && tx.Inputs[0].Out == -1
}

func (tx *Transaction) Sign(privKey *ecdsa.PrivateKey, prevTXs map[string]Transaction) {
	if tx.IsCoinBase() {
		return
	}

	for _, in := range tx.Inputs {
		if prevTXs[hex.EncodeToString(in.ID)].ID == nil {
			log.Panic("ERROR: Previous transaction is not correct")
		}
	}

	txCopy := tx.TrimmedCopy()

	for inId, in := range txCopy.Inputs {
		prevTX := prevTXs[hex.EncodeToString(in.ID)]
		txCopy.Inputs[inId].Signature = nil
		txCopy.Inputs[inId].PubKey = prevTX.Outputs[in.Out].PubKeyHash
		txCopy.ID = txCopy.Hash()
		txCopy.Inputs[inId].PubKey = nil

		r, s, err := ecdsa.Sign(rand.Reader, privKey, txCopy.ID)
		common.HandlerError(err)
		signature := append(r.Bytes(), s.Bytes()...)

		tx.Inputs[inId].Signature = signature

	}
}


func (tx *Transaction) Verify(prevTXs map[string]Transaction) bool {
	if tx.IsCoinBase() {
		return true
	}

	for _, in := range tx.Inputs {
		if prevTXs[hex.EncodeToString(in.ID)].ID == nil {
			log.Panic("Previous transaction not correct")
		}
	}

	txCopy := tx.TrimmedCopy()
	curve := elliptic.P256()

	for inId, in := range tx.Inputs {
		prevTx := prevTXs[hex.EncodeToString(in.ID)]
		txCopy.Inputs[inId].Signature = nil
		txCopy.Inputs[inId].PubKey = prevTx.Outputs[in.Out].PubKeyHash
		txCopy.ID = txCopy.Hash()
		txCopy.Inputs[inId].PubKey = nil

		r := big.Int{}
		s := big.Int{}

		sigLen := len(in.Signature)
		r.SetBytes(in.Signature[:(sigLen / 2)])
		s.SetBytes(in.Signature[(sigLen / 2):])

		x := big.Int{}
		y := big.Int{}
		keyLen := len(in.PubKey)
		x.SetBytes(in.PubKey[:(keyLen / 2)])
		y.SetBytes(in.PubKey[(keyLen / 2):])
		rawPubKey := ecdsa.PublicKey{
			Curve: curve,
			X:     &x,
			Y:     &y,
		}
		return ecdsa.Verify(&rawPubKey, txCopy.ID, &r, &s) 
	}

	return true
}

func (tx *Transaction) TrimmedCopy() Transaction {
	var inputs []TxInput
	var outputs []TxOutput

	for _, in := range tx.Inputs {
		inputs = append(inputs, TxInput{in.ID, in.Out, nil, nil})
	}

	for _, out := range tx.Outputs {
		outputs = append(outputs, TxOutput{out.Value, out.PubKeyHash})
	}

	txCopy := Transaction{tx.ID, inputs, outputs}

	return txCopy
}

func (tx Transaction) Serialize() []byte {
	var encoded bytes.Buffer

	enc := gob.NewEncoder(&encoded)
	err := enc.Encode(tx)
	if err != nil {
		log.Panic(err)
	}

	return encoded.Bytes()
}


func NewTransaction(from, to string, amount int, UTXO *UTXOSet) *Transaction {
	var inputs []TxInput
	var outputs []TxOutput

	wallets, err := wallet.CreateWallets()
	common.HandlerError(err)
	w := wallets.GetWallet(from)
	pubKeyHash := wallet.PublicKeyHash(w.PublicKey)
	acc, validOutputs := UTXO.FindSpendableOutputs(pubKeyHash, amount)

	if acc < amount {
		log.Panic("Error: not enough funds")
	}

	for txid, outs := range validOutputs {
		txID, err := hex.DecodeString(txid)
		common.HandlerError(err)

		for _, out := range outs {
			input := TxInput{txID, out, nil, w.PublicKey}
			inputs = append(inputs, input)
		}
	}

	outputs = append(outputs, *NewTXOutput(amount, to))

	if acc > amount {
		outputs = append(outputs, *NewTXOutput(acc-amount, from))
	}

	tx := Transaction{nil, inputs, outputs}
	tx.ID = tx.Hash()
	privateKey, err := x509.ParseECPrivateKey( w.PrivateKey)
	if err != nil {
		common.HandlerError(err)
	}
	UTXO.Blockchain.SignTransaction(&tx, privateKey)

	return &tx
}


/*
Every transaction in Bitcoin has a unique ID (TxID).
This ID is not random; it is obtained by serializing the transaction data into bytes and then computing its hash.
Therefore, the TxID serves as a “fingerprint” of the transaction’s content.
*/
// SetID, creates hash for this transaction
func (tx *Transaction) SetID() {
	var encoded bytes.Buffer
	var hash [32]byte
	encode := gob.NewEncoder(&encoded)
	err := encode.Encode(tx)
	common.HandlerError(err)
	hash = sha256.Sum256(encoded.Bytes())
	tx.ID = hash[:]
}



// CoinbaseTx, Coinbase 交易直接生成币，不消耗 UTXO
func CoinbaseTx(to, data string) *Transaction{
	if data == "" {
		data = fmt.Sprintf("Coins to %s", to)
	}
	txin := TxInput{[]byte{}, -1, nil, []byte(data)} // 输入是“空引用”，索引 -1，Sig 可以写一些信息
	txout := NewTXOutput(100, to) // 输出给矿工 to，金额固定 100（这里简化了）
	tx := Transaction{nil, []TxInput{txin}, []TxOutput{*txout}}
	tx.SetID()
	return &tx
}


func (tx Transaction) String() string {
	var lines []string

	lines = append(lines, fmt.Sprintf("--- Transaction %x:", tx.ID))
	for i, input := range tx.Inputs {
		lines = append(lines, fmt.Sprintf("     Input %d:", i))
		lines = append(lines, fmt.Sprintf("       TXID:     %x", input.ID))
		lines = append(lines, fmt.Sprintf("       Out:       %d", input.Out))
		lines = append(lines, fmt.Sprintf("       Signature: %x", input.Signature))
		lines = append(lines, fmt.Sprintf("       PubKey:    %x", input.PubKey))
	}

	for i, output := range tx.Outputs {
		lines = append(lines, fmt.Sprintf("     Output %d:", i))
		lines = append(lines, fmt.Sprintf("       Value:  %d", output.Value))
		lines = append(lines, fmt.Sprintf("       Script: %x", output.PubKeyHash))
	}

	return strings.Join(lines, "\n")
}
