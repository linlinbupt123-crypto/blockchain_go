package network

/*
为区块链项目创建一个网络模块，使得每个节点都能独立存储区块链数据，并能够与其他节点通信。这个过程涉及到构建网络逻辑，并将其整合到区块链系统中。
中心节点（Central Node）：负责连接所有其他节点并在它们之间传递数据。
矿工节点（Minor Node）：存储交易并生成新区块。
钱包节点（Wallet Node）：用于在钱包之间发送加密货币，且持有完整的区块链副本。
*/

import (
	"bytes"
	"encoding/gob"
	"encoding/hex"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net"
	"os"
	"runtime"
	"syscall"

	death "github.com/vrecan/death/v3"

	"blockchain_go/blockchain"
	"blockchain_go/common"
)

const (
	protocol      = "tcp"
	version       = 1
	commandLength = 12
)

var (
	nodeAddress     string // 每个节点实例都会有一个唯一的地址，通常是通过端口号来区分。节点地址是唯一的，每个节点实例通过不同的端口号进行区分
	mineAddress     string // 表示作为矿工的节点地址，矿工负责挖矿和验证交易。处理交易和验证区块
	KnownNodes      = []string{"localhost:3000"} // 用来存储网络中已知的节点地址。这个数组将包含所有连接到该网络的本地主机地址（即，所有的节点地址）。
	blocksInTransit = [][]byte{} // 创建一个二维的 bit slice（比特切片）来表示区块数据，可能用于存储区块链中的具体区块信息。
	memoryPool      = make(map[string]blockchain.Transaction) // 为了存储交易数据，使用一个映射（map）来存储每一笔交易，map 的键是交易 ID，值是交易本身。
)

type Addr struct {
	AddrList []string
}

type Block struct {
	AddrFrom string
	Block    []byte
}

type GetBlocks struct {
	AddrFrom string
}

type GetData struct {
	AddrFrom string
	Type     string
	ID       []byte
}

type Inv struct {
	AddrFrom string
	Type     string
	Items    [][]byte
}

type Tx struct {
	AddrFrom    string
	Transaction []byte
}
/*
Version 用于区块链多节点之间的同步（sync）过程。
同步流程：
1. 节点之间互相发送自身区块链版本（BestHeight）
2. 比较区块高度（链长度）
3. 如果本地区块链落后，则向对方请求缺失的区块
4. 直至所有节点区块高度一致

说明：
- BestHeight 表示该节点当前区块链的高度（区块总数）
- Version 可用于标识该节点的版本号（可与 BestHeight 相同或用于扩展）
- AddrFrom 表示发送 version 消息的节点地址
*/
type Version struct {
	Version    int
	BestHeight int
	AddrFrom   string
}

func CmdToBytes(cmd string) []byte {
	var bytes [commandLength]byte

	for i, c := range cmd {
		bytes[i] = byte(c)
	}

	return bytes[:]
}

func BytesToCmd(bytes []byte) string {
	var cmd []byte

	for _, b := range bytes {
		if b != 0x0 {
			cmd = append(cmd, b)
		}
	}

	return fmt.Sprintf("%s", cmd)
}

func ExtractCmd(request []byte) []byte {
	return request[:commandLength]
}

func RequestBlocks() {
	for _, node := range KnownNodes {
		SendGetBlocks(node)
	}
}

func SendAddr(address string) {
	nodes := Addr{KnownNodes}
	nodes.AddrList = append(nodes.AddrList, nodeAddress)
	payload := GobEncode(nodes)
	request := append(CmdToBytes("addr"), payload...)

	SendData(address, request)
}

func SendBlock(addr string, b *blockchain.Block) {
	data := Block{nodeAddress, b.Serialize()}
	payload := GobEncode(data)
	request := append(CmdToBytes("block"), payload...)

	SendData(addr, request)
}

func SendData(addr string, data []byte) {
	conn, err := net.Dial(protocol, addr)

	if err != nil {
		fmt.Printf("%s is not available\n", addr)
		var updatedNodes []string

		for _, node := range KnownNodes {
			if node != addr {
				updatedNodes = append(updatedNodes, node)
			}
		}

		KnownNodes = updatedNodes

		return
	}

	defer conn.Close()

	_, err = io.Copy(conn, bytes.NewReader(data))
	if err != nil {
		log.Panic(err)
	}
}

func SendInv(address, kind string, items [][]byte) {
	inventory := Inv{nodeAddress, kind, items}
	payload := GobEncode(inventory)
	request := append(CmdToBytes("inv"), payload...)

	SendData(address, request)
}

func SendGetBlocks(address string) {
	payload := GobEncode(GetBlocks{nodeAddress})
	request := append(CmdToBytes("getblocks"), payload...)

	SendData(address, request)
}

func SendGetData(address, kind string, id []byte) {
	payload := GobEncode(GetData{nodeAddress, kind, id})
	request := append(CmdToBytes("getdata"), payload...)

	SendData(address, request)
}

func SendTx(addr string, tnx *blockchain.Transaction) {
	data := Tx{nodeAddress, tnx.Serialize()}
	payload := GobEncode(data)
	request := append(CmdToBytes("tx"), payload...)

	SendData(addr, request)
}

func SendVersion(addr string, chain *blockchain.BlockChain) {
	bestHeight, err := chain.GetBestHeight()
	if err != nil{
		common.HandlerError(err)
	}
	payload := GobEncode(Version{version, bestHeight, nodeAddress})

	request := append(CmdToBytes("version"), payload...)

	SendData(addr, request)
}

func HandleAddr(request []byte) {
	var buff bytes.Buffer
	var payload Addr

	buff.Write(request[commandLength:])
	dec := gob.NewDecoder(&buff)
	err := dec.Decode(&payload)
	if err != nil {
		log.Panic(err)

	}

	KnownNodes = append(KnownNodes, payload.AddrList...)
	fmt.Printf("there are %d known nodes\n", len(KnownNodes))
	RequestBlocks()
}
// HandleBlock 处理来自其他节点发送的区块数据（block 命令）。
//
// 流程说明：
// 1. 从收到的字节流中读取命令并提取 payload。
// 2. 使用 gob 解码 payload 得到区块（Block）。
// 3. 调用 DeserializeBlock 将字节转换为 Block 对象。
// 4. 打印收到新区块的日志，便于调试和观察节点间同步。
// 5. 调用 AddBlock 将该区块加入本地区块链。
// 6. 若还有未下载的区块（blocksInTransit 列表中），则：
//      - 取出下一个区块哈希
//      - 发送 getblock 请求以获取该区块
//      - blocksInTransit 向前移动（下标 0 的已被处理）
// 7. 若没有更多区块需要下载，则：
//      - 创建 UTXOSet
//      - 重新索引 UTXO（Reindex），确保新链结构正确。
// 
// 该函数用于链同步流程：当节点收到一个区块后，会自动继续拉取剩余区块，直到全部同步。
func HandleBlock(request []byte, chain *blockchain.BlockChain) {
	var buff bytes.Buffer
	var payload Block

	buff.Write(request[commandLength:])
	dec := gob.NewDecoder(&buff)
	err := dec.Decode(&payload)
	if err != nil {
		log.Panic(err)
	}

	blockData := payload.Block
	block := blockchain.Deserialize(blockData)

	fmt.Println("Recevied a new block!")
	chain.AddBlock(block)

	fmt.Printf("Added block %x\n", block.Hash)

	if len(blocksInTransit) > 0 {
		blockHash := blocksInTransit[0]
		SendGetData(payload.AddrFrom, "block", blockHash)

		blocksInTransit = blocksInTransit[1:]
	} else {
		UTXOSet := blockchain.UTXOSet{Blockchain:chain}
		UTXOSet.Reindex()
	}
}

func HandleInv(request []byte, chain *blockchain.BlockChain) {
	var buff bytes.Buffer
	var payload Inv

	buff.Write(request[commandLength:])
	dec := gob.NewDecoder(&buff)
	err := dec.Decode(&payload)
	if err != nil {
		log.Panic(err)
	}

	fmt.Printf("Recevied inventory with %d %s\n", len(payload.Items), payload.Type)

	if payload.Type == "block" {
		blocksInTransit = payload.Items

		blockHash := payload.Items[0]
		SendGetData(payload.AddrFrom, "block", blockHash)

		newInTransit := [][]byte{}
		for _, b := range blocksInTransit {
			if bytes.Compare(b, blockHash) != 0 {
				newInTransit = append(newInTransit, b)
			}
		}
		blocksInTransit = newInTransit
	}

	if payload.Type == "tx" {
		txID := payload.Items[0]

		if memoryPool[hex.EncodeToString(txID)].ID == nil {
			SendGetData(payload.AddrFrom, "tx", txID)
		}
	}
}

func HandleGetBlocks(request []byte, chain *blockchain.BlockChain) {
	var buff bytes.Buffer
	var payload GetBlocks

	buff.Write(request[commandLength:])
	dec := gob.NewDecoder(&buff)
	err := dec.Decode(&payload)
	if err != nil {
		log.Panic(err)
	}

	blocks := chain.GetBlockHashes()
	SendInv(payload.AddrFrom, "block", blocks)
}

func HandleGetData(request []byte, chain *blockchain.BlockChain) {
	var buff bytes.Buffer
	var payload GetData

	buff.Write(request[commandLength:])
	dec := gob.NewDecoder(&buff)
	err := dec.Decode(&payload)
	if err != nil {
		log.Panic(err)
	}

	if payload.Type == "block" {
		block, err := chain.GetBlock([]byte(payload.ID))
		if err != nil {
			return
		}

		SendBlock(payload.AddrFrom, &block)
	}

	if payload.Type == "tx" {
		txID := hex.EncodeToString(payload.ID)
		tx := memoryPool[txID]

		SendTx(payload.AddrFrom, &tx)
	}
}
// HandleTx 处理接收到的交易，并根据条件广播或挖矿。
//
// 流程说明：
// 1. 接收来自网络的交易字节流。
// 2. 将字节流解码为 Transaction 结构体。
// 3. 将交易存入内存池（Memory Pool），
//      - key 为交易 ID（string 编码）
//      - value 为交易本身
// 4. 如果当前节点是中心节点（Central Node）：
//      - 遍历已知节点列表（knownNodes）
//      - 将交易广播给除中心节点和矿工节点以外的其他节点
// 5. 如果当前节点是矿工节点（Minor Node）：
//      - 检查内存池中交易数量是否超过阈值（例如 > 2）
//      - 检查是否存在矿工节点地址（minor address）
//      - 如果条件满足，调用 MineTransaction() 生成新区块
//          - MineTransaction 会从内存池选择交易打包
//          - 更新区块链（Blockchain）
//          - 更新 UTXOSet
//          - 从内存池中移除已打包交易
//
// 注意：
// - Memory Pool 用于暂存未打包交易，为矿工挖矿提供数据。
// - 广播机制确保交易能传播到网络中其他节点。
// - 挖矿条件可根据实际需求调整阈值。
func HandleTx(request []byte, chain *blockchain.BlockChain) {
	var buff bytes.Buffer
	var payload Tx

	buff.Write(request[commandLength:])
	dec := gob.NewDecoder(&buff)
	err := dec.Decode(&payload)
	if err != nil {
		log.Panic(err)
	}

	txData := payload.Transaction
	tx := blockchain.DeserializeTransaction(txData)
	memoryPool[hex.EncodeToString(tx.ID)] = tx

	fmt.Printf("%s, %d", nodeAddress, len(memoryPool))

	if nodeAddress == KnownNodes[0] {
		for _, node := range KnownNodes {
			if node != nodeAddress && node != payload.AddrFrom {
				SendInv(node, "tx", [][]byte{tx.ID})
			}
		}
	} else {
		if len(memoryPool) >= 2 && len(mineAddress) > 0 {
			MineTx(chain)
		}
	}
}
// MineTx 处理内存池中的交易并生成新区块（挖矿流程）。
//
// 流程说明：
// 1. 遍历内存池（Memory Pool）中的交易。
//      - 对每笔交易进行验证（VerifyTransaction）
//      - 将通过验证的交易加入临时交易列表
//      - 若所有交易无效，则停止挖矿
// 2. 创建 Coinbase 交易（奖励交易），矿工地址为 minor address
//      - 将 Coinbase 交易加入临时交易列表
// 3. 调用 MineBlock(transactions) 生成新区块，并添加到区块链
// 4. 更新 UTXOSet（UTXOSet.Reindex()）
// 5. 从内存池中删除已打包的交易
// 6. 广播新区块给所有已知节点（knownNodes），更新它们的区块链
// 7. 如果内存池中仍有交易，则递归调用 MineTransaction() 继续挖矿
//
// 注意：
// - Memory Pool 存储所有未打包交易，是矿工挖矿的交易来源
// - Coinbase 交易保证矿工获得区块奖励
// - 广播机制确保新区块在网络中同步
// 矿工挖矿流程 = 验证交易 → 添加奖励交易 → 生成新区块 → 更新 UTXO → 清理内存池 → 广播新区块 → 递归挖矿（如果内存池仍有交易）
func MineTx(chain *blockchain.BlockChain) {
	var txs []*blockchain.Transaction

	for id := range memoryPool {
		fmt.Printf("tx: %s\n", memoryPool[id].ID)
		tx := memoryPool[id]
		if chain.VerifyTransaction(&tx) {
			txs = append(txs, &tx)
		}
	}

	if len(txs) == 0 {
		fmt.Println("All Transactions are invalid")
		return
	}

	cbTx := blockchain.CoinbaseTx(mineAddress, "")
	txs = append(txs, cbTx)

	newBlock := chain.MineBlock(txs)
	UTXOSet  := blockchain.UTXOSet{Blockchain:chain}
	UTXOSet.Reindex()

	fmt.Println("New Block mined")

	for _, tx := range txs {
		txID := hex.EncodeToString(tx.ID)
		delete(memoryPool, txID)
	}

	for _, node := range KnownNodes {
		if node != nodeAddress {
			SendInv(node, "block", [][]byte{newBlock.Hash})
		}
	}

	if len(memoryPool) > 0 {
		MineTx(chain)
	}
}
// HandleVersion 处理来自其他节点的 version 消息，用于区块链同步。
// 
// 流程说明：
// 1. 接收并解码对方节点发送的 Version 消息。
// 2. 获取本地区块链的高度（BestHeight）。
// 3. 比较本地链高度与对方链高度：
//      - 如果本地高度 < 对方高度：
//            -> 本地缺少新区块，发送 getblocks 命令请求下载缺失区块。
//      - 如果本地高度 > 对方高度：
//            -> 本地区块链更长，调用 sendVersion(peer, blockchain) 将版本信息发送给对方，方便对方同步。
// 4. 检查节点是否已经存在于已知节点列表（knownNodes）：
//      - 如果节点不存在，则将其加入 knownNodes。
// 5. 该函数同时为后续交易处理做准备，确保节点能够接收和广播交易。
//
// 注意：
// - Version 结构体中 BestHeight 字段表示节点当前区块链高度。
// - knownNodes 用于维护网络中已知节点列表，避免重复请求或广播。
func HandleVersion(request []byte, chain *blockchain.BlockChain) {
	var buff bytes.Buffer
	var payload Version

	buff.Write(request[commandLength:])
	dec := gob.NewDecoder(&buff)
	err := dec.Decode(&payload)
	if err != nil {
		log.Panic(err)
	}

	bestHeight ,_:= chain.GetBestHeight()
	otherHeight := payload.BestHeight

	if bestHeight < otherHeight {
		SendGetBlocks(payload.AddrFrom)
	} else if bestHeight > otherHeight {
		SendVersion(payload.AddrFrom, chain)
	}

	if !NodeIsKnown(payload.AddrFrom) {
		KnownNodes = append(KnownNodes, payload.AddrFrom)
	}
}

func HandleConnection(conn net.Conn, chain *blockchain.BlockChain) {
	req, err := ioutil.ReadAll(conn)
	defer conn.Close()
	
	if err != nil {
		log.Panic(err)
	}
	command := BytesToCmd(req[:commandLength])
	fmt.Printf("Received %s command\n", command)

	switch command {
	case "addr":
		HandleAddr(req)
	case "block":
		HandleBlock(req, chain)
	case "inv":
		HandleInv(req, chain)
	case "getblocks":
		HandleGetBlocks(req, chain)
	case "getdata":
		HandleGetData(req, chain)
	case "tx":
		HandleTx(req, chain)
	case "version":
		HandleVersion(req, chain)
	default:
		fmt.Println("Unknown command")
	}

}

func StartServer(nodeID, minerAddress string) {
	nodeAddress = fmt.Sprintf("localhost:%s", nodeID)
	mineAddress = minerAddress
	ln, err := net.Listen(protocol, nodeAddress)
	if err != nil {
		log.Panic(err)
	}
	defer ln.Close()

	chain,_ := blockchain.ContinueBlockChain(nodeID)
	defer chain.Database.Close()
	go CloseDB(chain)

	if nodeAddress != KnownNodes[0] {
		SendVersion(KnownNodes[0], chain)
	}
	for {
		conn, err := ln.Accept()
		if err != nil {
			log.Panic(err)
		}
		go HandleConnection(conn, chain)

	}
}

func GobEncode(data interface{}) []byte {
	var buff bytes.Buffer

	enc := gob.NewEncoder(&buff)
	err := enc.Encode(data)
	if err != nil {
		log.Panic(err)
	}

	return buff.Bytes()
}

func NodeIsKnown(addr string) bool {
	for _, node := range KnownNodes {
		if node == addr {
			return true
		}
	}

	return false
}

// CloseDB 监听系统退出信号并安全关闭区块链数据库。
// 
// 区块链节点在运行期间会持有 BadgerDB，如果用户按下 Ctrl+C 或系统发送终止信号，
// 必须优雅关闭数据库，否则可能造成数据损坏。
// 
// 本函数使用 death 库来拦截不同操作系统可能触发的退出信号：
//   - syscall.SIGINT / os.Interrupt：用户按下 Ctrl+C（Linux / macOS）
//   - syscall.SIGTERM：系统终止信号（Linux / macOS）
//   - Windows 使用 os.Interrupt 作为兼容信号
// 
// 捕获到退出信号后：
//   1. 关闭 BadgerDB（chain.Database.Close()）
//   2. 退出当前 goroutine（runtime.Goexit）
//   3. 退出整个程序（os.Exit）
// 
// 这样可确保节点在关闭时不会损坏数据库文件。

func CloseDB(chain *blockchain.BlockChain) {
	d := death.NewDeath(syscall.SIGINT, syscall.SIGTERM, os.Interrupt)

	d.WaitForDeathWithFunc(func() {
		defer os.Exit(1)
		defer runtime.Goexit()
		chain.Database.Close()
	})
}