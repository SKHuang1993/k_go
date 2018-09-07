package main

import (
	//"github.com/davyxu/golog"
	"internal/kklog"
//	"github.com/davyxu/golog"
	"fmt"

	"os"
)


var log = kklog.New("test2")

const colorStyle = `
{

"Rule":[
		{"Text":"panic:","Color":"Red"},
		{"Text":"[DB]","Color":"Green"},
		{"Text":"#http.listen","Color":"Blue"},
		{"Text":"#http.recv","Color":"Blue"},
		{"Text":"#http.send","Color":"Purple"},
		{"Text":"#tcp.listen","Color":"Blue"},
		{"Text":"#tcp.accepted","Color":"Blue"},
		{"Text":"#tcp.closed","Color":"Blue"},
		{"Text":"#tcp.recv","Color":"Blue"},
		{"Text":"#tcp.send","Color":"Purple"},
		{"Text":"#tcp.connected","Color":"Blue"},
		{"Text":"#udp.listen","Color":"Blue"},
		{"Text":"#udp.recv","Color":"Blue"},
		{"Text":"#udp.send","Color":"Purple"},
		{"Text":"#rpc.recv","Color":"Blue"},
		{"Text":"#rpc.send","Color":"Purple"},
		{"Text":"#relay.recv","Color":"Blue"},
		{"Text":"#relay.send","Color":"Purple"}
	]
}
`


func  main() {



	kklog.SetColorDefine(".", colorStyle)

	// 默认颜色是关闭的
	//log.SetParts()


	//如果false，则不会显示颜色出来
	kklog.EnableColorLogger(".", true)
	log.Debugln("关闭所有部分样式")


	log.SetParts(kklog.LogPart_Level)
	log.SetColor("green")
	log.Debugln("绿色的字+级别")
	log.Errorln("Error级别啊嗷嗷",fmt.Sprintf("test"))



	//后面 的kklog.LogPart_Time 决定是否要显示打印时间，  LogPart_ShortFileName 是否要显示具体文件具体哪一行
	log.SetParts(kklog.LogPart_Level, kklog.LogPart_Name, kklog.LogPart_Time, kklog.LogPart_ShortFileName)
	log.SetColor("green")
	 file,err :=os.Open("1.txt")
	if err!=nil{
		log.Warnln("打开文件错误")
	}

	defer file.Close()



	log.SetParts(kklog.LogPart_Level, kklog.LogPart_Name)
	// 颜色只会影响一行
	log.SetColor("blue")
	log.Warnf("级别颜色高于手动设置 + 日志名字")



	log.SetParts(kklog.LogPart_Level, kklog.LogPart_Name, kklog.LogPart_Time, kklog.LogPart_ShortFileName)
	 log.SetColor("green")
	log.Debugf("[DB] DB日志是绿色的，从文件读取，按文字匹配的， 完整的日志样式")


	log.SetParts(kklog.LogPart_TimeMS, kklog.LogPart_LongFileName, func(l *kklog.Logger) {
		l.WriteRawString("固定头部: ")
	})



	log.SetColor("purple")
	log.Debugf("#http.send 哈哈哈")
	log.Debugf("自定义紫色 + 固定头部内容2")


}