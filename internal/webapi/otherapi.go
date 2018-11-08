package webapi

import (
	"bytes"
	"cacheflight"
	"cachestation"
	"compress/gzip"
	"encoding/json"
	"errorlog"
	"errors"
	"fmt"
	"io/ioutil"
	"mysqlop"
	"net/http"
	"outsideapi"
	"sort"
	"strings"
)

/****票单数据转入MySQL****/
/**
流程总结：
1。检查传入的参数，Deal是否为"GetBillData"
2.利用传入的BillID获取票单的数据（这个过程包括删除mysql中票单数据，获取最新windows数据，接着同步到mysql中）
3。利用传入的BillID获取票单的条款（这个过程包括删除mysql中条款数据，获取最新windows数据，接着同步到mysql中）
4。遍历数据库Service中B2FareReload=1的情况，将对应的服务器地址存起来
5。通过获取的服务器地址存起来，接着 "http://" + server + ":9999/ReloadB2Fare" 调用我们自己的内部接口，在ks_fare中声明的接口
6。通过传入的MainBillDelete，MainBillReload  这两步开始不清楚了
*/


func MainBill2MySQL(w http.ResponseWriter, r *http.Request) {
	defer errorlog.DealRecoverLog()

	r.ParseForm()
	result, _ := ioutil.ReadAll(r.Body)

	var ts cacheflight.TotleService

	if err := json.Unmarshal(result, &ts); err != nil {
		errorlog.WriteErrorLog("MainBill2MySQL (1): " + err.Error())
		fmt.Fprintf(w, errorlog.Result(false))
		return
	}
	if ts.Deal == "GetBillData" && mysqlop.GetMainBillData(ts.Code) {

		mysqlop.GetMainBillCondition(ts.Code)

		for _, server := range mysqlop.ServiceSelect("B2FareReload") {
			mysqlop.ReloadB2Fare(server, ts.Code)
		}
		fmt.Fprintf(w, errorlog.Result(true))
	} else {
		fmt.Fprintf(w, errorlog.Result(false))
	}

}

/***删除某一票单数据***/
/**
流程总结：
1。检查传入的参数，Deal是否为"B2FareDelete"
2。遍历数据库Service中B2FareDelete=1的情况，将对应的服务器地址存起来
3。通过获取的服务器地址存起来，接着 "http://" + server + ":9999/DeleteB2Fare" 调用我们自己的内部接口，在ks_fare中声明的接口
4。进入到fare.DeleteB2Fare 这个函数。但是接下来的操作
5。MainBillDelete(ts.Code) 进行票单删除的操作


*/

func MySQL2DeleteMainBill(w http.ResponseWriter, r *http.Request) {
	defer errorlog.DealRecoverLog()

	r.ParseForm()
	result, _ := ioutil.ReadAll(r.Body)

	var ts cacheflight.TotleService
	if err := json.Unmarshal(result, &ts); err != nil {
		errorlog.WriteErrorLog("MySQL2DeleteMainBill (1): " + err.Error())
		fmt.Fprintf(w, errorlog.Result(false))
		return
	}

	if ts.Deal == "DeleteBillData" && len(ts.Code) > 10 { //先删除票单后其实就没办法删除缓存了,因为读不到数据库信息
		for _, server := range mysqlop.ServiceSelect("B2FareDelete") {
			mysqlop.DeleteB2Fare(server, ts.Code)
		}
		mysqlop.DeleteMainBillData(ts.Code) //这句应该方后面
		fmt.Fprintf(w, errorlog.Result(true))
	} else {
		fmt.Fprintf(w, errorlog.Result(false))
	}
}

/***删除所有票单数据***/
/**
流程总结


流程总结：
1。检查传入的参数，Deal是否为"DeleteBillData"
2。遍历数据库Service中B2FareDelete=1的情况，将对应的服务器地址存起来
3。通过获取的服务器地址存起来，接着 "http://" + server + ":9999/DeleteAllB2Fare" 调用我们自己的内部接口，在ks_fare中声明的接口
4。进入到fare.DeleteAllB2Fare 这个函数。但是接下来的操作
5。里面的操作看不懂
*/
func MySQL2DeleteAllMainBill(w http.ResponseWriter, r *http.Request) {
	defer errorlog.DealRecoverLog()

	r.ParseForm()
	result, _ := ioutil.ReadAll(r.Body)

	var ts cacheflight.TotleService
	if err := json.Unmarshal(result, &ts); err != nil {
		errorlog.WriteErrorLog("MySQL2DeleteAllMainBill (1): " + err.Error())
		fmt.Fprintf(w, errorlog.Result(false))
		return
	}

	if ts.Deal == "DeleteAllBillData" && mysqlop.DeleteAllMainBillData() {
		for _, server := range mysqlop.ServiceSelect("B2FareDeleteAll") {
			mysqlop.DeleteAllB2Fare(server)
		}
		fmt.Fprintf(w, errorlog.Result(true))
	} else {
		fmt.Fprintf(w, errorlog.Result(false))
	}
}

/***获取票单条款WAPI_***/
func GetCondition(w http.ResponseWriter, r *http.Request) {
	defer errorlog.DealRecoverLog()

	r.ParseForm()
	result, _ := ioutil.ReadAll(r.Body)

	var ts cacheflight.TotleService
	if err := json.Unmarshal(result, &ts); err != nil {
		errorlog.WriteErrorLog("GetCondition (1): " + err.Error())
		fmt.Fprintf(w, errorlog.Result(false))
		return
	}

	if ts.Deal == "GetCondition" {

		fmt.Fprintf(w, mysqlop.GetCondition(ts.Code))

		//输入
		/**
		这个是conditionID:"38EA8B8D-C3FA-4FDF-A55B-58DF33D66A7B"
		*/

		//输出
		/**
		1、适用性 1.1、此票价为海天一票通特惠价，含往返船票和机票。 1.2、此特惠价同时适用以下两种行程：
		1.3、行程1：莲花山港-X/香港-目的地-X/香港-广州。 1.4、行程2：莲花山港-X/香港-目的地-X/香港-莲花山港。
		1.5、莲花山港往返香港航
		3.1、不允许停留香港。
		4、构成和组合 4.1、允许票价组合，规则以最严格的为准，详情请致电查询。
		5、梗改/取消附加费 5.1、完全未使用可退票，退票费以出票时确认为准，不可部份退票。 5.2、须提供预先取消机位的证明，否则需额外收取误订座更改后当天完成重开机票或连接票号等手续。 5.6、须在出发日之前取消原订航班，否则视为误机。 5.7、误机每次需收取CNY1200。 5.8、误机后改期或退票，需收取误机费加改期费或退票费。
		6、机票签注 6.1、不可签转，不可更改行程，限乘公司燃油附加费及导航税）。
		8.2、票价如有更改，恕不另行通知。
		*/

	} else {
		fmt.Fprintf(w, errorlog.Result(false))
	}
}

/****获取DiscountCode信息****/
/**
流程总结：
1。检查传入的参数，Deal是否为"GetDiscountCode"
2.利用传入的ID获取折扣代码的数据（这个过程包括删除mysql中折扣代码数据，获取最新windows数据，接着同步到mysql中）
3。遍历数据库Service中DiscountReload=1的情况，将对应的服务器地址存起来（查了之后发现是110，113这两台）
4。通过获取的服务器地址存起来，接着 http://" + server + ":8888/ReloadDiscountCode 调用我们自己的内部接口，在ks_server ks_discountcode 中声明的接口
5。进入到mysqlop.DCDelete(ts.Code) //删除内存（删除对应的折扣代码）,将折扣代码中的各种类型都删除
6.go mysqlop.DCSelect(ts.Code) //接着再进行折扣代码的select。 利用这个最新的code去查询折扣代码，接着将数据存到内存中
7。5。6这两个看不懂

*/

func DiscountCode2MySQL(w http.ResponseWriter, r *http.Request) {
	defer errorlog.DealRecoverLog()

	r.ParseForm()
	result, _ := ioutil.ReadAll(r.Body)

	var ts cacheflight.TotleService
	if err := json.Unmarshal(result, &ts); err != nil {
		errorlog.WriteErrorLog("DiscountCode2MySQL (1): " + err.Error())
		fmt.Fprintf(w, errorlog.Result(false))
		return
	}

	if ts.Deal == "GetDiscountCode" && mysqlop.GetDiscountCodeAir(ts.Code) {

		//在这里将数据加载进内存，主动调用
		for _, server := range mysqlop.ServiceSelect("DiscountReload") {
			mysqlop.ReloadDiscountCode(server, ts.Code)
		}

		fmt.Fprintf(w, errorlog.Result(true))
	} else {
		fmt.Fprintf(w, errorlog.Result(false))
	}
}

/****删除DiscountCode信息****/
/**
流程
1。mysql中删除对应的code的折扣代码
2。遍历数据库Service中DiscountDelete=1的情况，将对应的服务器地址存起来（查了之后发现是110，113这两台）
3。 通过获取的服务器地址存起来，接着 http://" + server + ":8888/DeleteDiscountCode
4。


*/
func MySQL2DeleteDiscountCode(w http.ResponseWriter, r *http.Request) {
	defer errorlog.DealRecoverLog()

	r.ParseForm()
	result, _ := ioutil.ReadAll(r.Body)

	var ts cacheflight.TotleService
	if err := json.Unmarshal(result, &ts); err != nil {
		errorlog.WriteErrorLog("MySQL2DeleteDiscountCode (1): " + err.Error())
		fmt.Fprintf(w, errorlog.Result(false))
		return
	}

	if ts.Deal == "DeleteDiscountCode" && mysqlop.DeleteDiscountCodeAir(ts.Code) {
		for _, server := range mysqlop.ServiceSelect("DiscountDelete") {
			mysqlop.DeleteDiscountCode(server, ts.Code)
		}

		fmt.Fprintf(w, errorlog.Result(true))
	} else {
		fmt.Fprintf(w, errorlog.Result(false))
	}
}

/****获取数据源配置信息****/
/**
流程
1。检查传入的参数，Deal是否为"GetDataSource"
2。紧着进行下面几个（进行插入）
DSKeyCacheInsert   获取windows的数据源，接着同步到mysql下面DataSourceKeyCache这个表
DSDCInsert          获取windows的数据源，接着同步mysql下面这个DataSource表。貌似数据缓存之类的
AgencyGroupInsert   获取windows的数据源，接着同步mysql下面AgencyGroup这个表。(这个表不懂....)
AgRoutineInsert    获取windows的数据源，接着同步mysql下面AgRoutine这个表。(这个表不懂....)
+------------------+--------------------------+----+
| ShowShoppingName | Routine                  | PS |
+------------------+--------------------------+----+
| FBFB             | CAN-KA-HKG-HX-BKK-HX-VTE |  0 (前缀。1则后缀)|
| FBFB             | PEK-CA-LAX-UA-SFO        |  0 |
+------------------+--------------------------+----+

处理完之后在110。113。114 这三台机器上http://" + server + ":8888/ReloadDataSource"
接着调用webapi.ReloadDataSource()
依然去做上面那4个操作。最终都是把结果存到缓存里面去
            mysqlop.DSKeyCacheSelect()
			mysqlop.DSDCSelect()
			mysqlop.AgencyGroupSelect()
			mysqlop.AgRoutineSelect()

*/



//#TODO 20181106 修改 新增对下面四个方法的调用

func SKDSKeyCacheInsert(w http.ResponseWriter, r *http.Request)  {

	r.ParseForm()
	result, _ := ioutil.ReadAll(r.Body)
	fmt.Println("同步数据---- SKDSKeyCacheInsert",string(result))
	var ts cacheflight.TotleService
	if err := json.Unmarshal(result, &ts); err != nil {
		errorlog.WriteErrorLog("SKDSKeyCacheInsert (1): " + err.Error())
		fmt.Fprintf(w, errorlog.Result(false))
		return
	}
      if b :=	mysqlop.DSKeyCacheInsert(); b{

      	fmt.Fprintf(w,"DSKeyCacheInsert --- 成功")

	  }else{
		  fmt.Fprintf(w,"DSKeyCacheInsert --- 失败")

	  }


}





//#TODO 20181105 修改 如果同步数据的话，需要下面几个数据源都没问题才可以往下跑
func DataSource2MySQL(w http.ResponseWriter, r *http.Request) {
	defer errorlog.DealRecoverLog()

	r.ParseForm()
	result, _ := ioutil.ReadAll(r.Body)

	//#TODO 20181105新加入(这个接口每次都是返回false，是否其中某一步出了问题)
	fmt.Println("同步数据---- DataSource2MySQL",string(result))

	var ts cacheflight.TotleService
	if err := json.Unmarshal(result, &ts); err != nil {
		errorlog.WriteErrorLog("DataSource2MySQL (1): " + err.Error())
		fmt.Fprintf(w, errorlog.Result(false))
		return
	}
	fmt.Println("开始调用 GetDataSource --DSKeyCacheInsert  DSDCInsert  AgencyGroupInsert  AgRoutineInsert---")
	//下面四个操作都是相对比较费时，是否对调用结果有影响
	if ts.Deal == "GetDataSource" &&
		mysqlop.DSKeyCacheInsert() &&
		mysqlop.DSDCInsert() &&
		mysqlop.AgencyGroupInsert() &&
		mysqlop.AgRoutineInsert() {

		fmt.Println("结束调用 GetDataSource --DSKeyCacheInsert  DSDCInsert  AgencyGroupInsert  AgRoutineInsert---")


		//自己调用自己，将mysql的数据加载进自身内存
		for _, server := range mysqlop.ServiceSelect("SourceReload") {

			//110,113,114 返回这三个服务器地址
			mysqlop.ReloadDataSource(server)
		}


		fmt.Fprintf(w, errorlog.Result(true))
	} else {
		fmt.Fprintf(w, errorlog.Result(false))
	}
}

/****从新读取税费并加载ks_odbc****/
/**
流程
1。检查传入的参数，Deal是否为"ReadReloadTax"
2。连接以前小林的数据库，获取最新数据，删除mysql下面的数据，接着将小林最新的数据存到linux下面的mysql上
3。遍历数据库Service中TaxReload=1的情况，将对应的服务器地址存起来（查了之后发现是110这两台）
4。 通过获取的服务器地址存起来，接着 http://" + server + ":8888/ReloadTax
5。调用了那个接口，接着跑mysqlop.TaxSelect()进行税费更新。加载到内存中(cachestation.Tax)这里面（税费只拿几个热门的1E,1B,1A,FB,1A）
*/

func ReadAndReloadTax(w http.ResponseWriter, r *http.Request) {
	defer errorlog.DealRecoverLog()

	r.ParseForm()
	result, _ := ioutil.ReadAll(r.Body)

	var ts cacheflight.TotleService
	if err := json.Unmarshal(result, &ts); err != nil {
		errorlog.WriteErrorLog("ReadAndReloadTax (1): " + err.Error())
		fmt.Fprintf(w, errorlog.Result(false))
		return
	}

	if ts.Deal == "ReadReloadTax" {
		go func() {
			if mysqlop.GetAllTax() {
				for _, server := range mysqlop.ServiceSelect("TaxReload") {
					mysqlop.ReloadTax(server)
				}
			}
		}()
		fmt.Fprintf(w, errorlog.Result(true))
	} else {
		fmt.Fprintf(w, errorlog.Result(false))
	}
}

/****查询出发地/目的地经过的中转地****/
type ConnectRoutine struct {
	DepartStation  string   `json:"DepartStation"`
	ConnectStation []string `json:"ConnectStation"`
	ArriveStation  string   `json:"ArriveStation"`
}

type ConnectOut struct {
	Rout []*ConnectRoutine `json:"Rout"`
}

//#TODO  东升.....  了解清楚
func QueryConnectStation(w http.ResponseWriter, r *http.Request) {

	defer errorlog.DealRecoverLog()

	connout := &ConnectOut{Rout: make([]*ConnectRoutine, 0, 1)}

	defer func() {
		fmt.Fprintf(w, string(errorlog.Make_JSON_GZip(connout)))
	}()

	r.ParseForm()

	result, _ := ioutil.ReadAll(r.Body)

	var (
		DepartStation []string
		ArriveStation []string
		ok            bool
		p2p           Point2Point_Flight
		legrout       string
	)

	if err := json.Unmarshal(result, &p2p); err != nil {
		errorlog.WriteErrorLog("QueryConnectStation (1): " + err.Error())
		return
	}

	//出发地城市对应到机场
	//传入p2p.DepartStation==出发城市；    通过出发城市找到对应的机场   DepartStation == 机场代码
	if DepartStation, ok = cachestation.CityCounty[p2p.DepartStation]; !ok {
		if _, ok = cachestation.County[p2p.DepartStation]; ok {
			DepartStation = []string{p2p.DepartStation}
		} else {
			DepartStation = []string{}
		}
	}
	//目的地城市对应到机场
	if ArriveStation, ok = cachestation.CityCounty[p2p.ArriveStation]; !ok {
		if _, ok = cachestation.County[p2p.ArriveStation]; ok {
			ArriveStation = []string{p2p.ArriveStation}
		} else {
			ArriveStation = []string{}
		}
	}

	for _, depart := range DepartStation {
		for _, arrive := range ArriveStation {
			if depart > arrive {
				legrout = arrive + depart
			} else {
				legrout = depart + arrive
			}

			connout.Rout = append(connout.Rout, &ConnectRoutine{
				DepartStation:  depart,
				ArriveStation:  arrive,
				ConnectStation: cachestation.Routine[legrout]})
		}
	}
}

/***重要的Web服务***/
/****ks_server向各ks_basic发送FlightJSON ****/
func FetchtFlightInfo(WebData []byte, ForB string) string {
	defer errorlog.DealRecoverLog()

	body := bytes.NewBuffer(WebData)
	gnr, err := gzip.NewReader(body)
	if err != nil {
		errorlog.WriteErrorLog("FetchtFlightInfo (1): " + err.Error())
		return errorlog.Result(false)
	}
	defer gnr.Close()
	result, _ := ioutil.ReadAll(gnr)

	var fjs cacheflight.FlightJSON

	if err := json.Unmarshal(result, &fjs); err != nil {
		errorlog.WriteErrorLog("FetchtFlightInfo (2): " + err.Error())
		return errorlog.Result(false)
	}

	//一层层遍历进去。如果发现里面的Leg或者Route长度为0的话，则默认其为不合法的，则return
	if len(fjs.Route) == 0 ||
		len(fjs.Route[0].FI) == 0 ||
		len(fjs.Route[0].FI[0].Legs) == 0 {
		return errorlog.Result(false)
	}
	leglen := len(fjs.Route[0].FI[0].Legs)

	if ForB == "F" { //这里是每办法过滤航段数的,因为fjs是非常多Rout组成
		if ip, ok := cachestation.PetchIP[fjs.Route[0].FI[0].Legs[0].DS]; ok {
			return outsideapi.SendFlightInfo(WebData, ip, "AcceptFlightInfoFore")
		}
	}

	if ForB == "B" { //这里是每办法过滤航段数的,因为fjs是非常多Rout组成
		if ip, ok := cachestation.PetchIP[fjs.Route[0].FI[0].Legs[leglen-1].AS]; ok {
			return outsideapi.SendFlightInfo(WebData, ip, "AcceptFlightInfoBack")
		}
	}

	if ForB == "ALL" {

		//查看我们现在要查询的航班数据里面的第一段的出发地是那台服务器。接着将航班数据，对应的IP地址，以及传入"AcceptFlightInfoFore"  去程
		if ip, ok := cachestation.PetchIP[fjs.Route[0].FI[0].Legs[0].DS]; ok {
			outsideapi.SendFlightInfo(WebData, ip, "AcceptFlightInfoFore")
		}
		//查看我们现在要查询的航班数据里面的第一段的目的地是那台服务器。接着将航班数据，对应的IP地址，以及传入"AcceptFlightInfoBack"  去程
		if ip, ok := cachestation.PetchIP[fjs.Route[0].FI[0].Legs[leglen-1].AS]; ok {
			outsideapi.SendFlightInfo(WebData, ip, "AcceptFlightInfoBack")
		}
		return errorlog.Result(true)
	}

	return errorlog.Result(false)
}

func FetchtFlightInfoFore(w http.ResponseWriter, r *http.Request) {
	defer errorlog.DealRecoverLog()

	r.ParseForm()
	result, _ := ioutil.ReadAll(r.Body)
	fmt.Fprintf(w, FetchtFlightInfo(result, "F"))
}

//
func FetchtFlightInfoBack(w http.ResponseWriter, r *http.Request) {

	defer errorlog.DealRecoverLog()
	r.ParseForm()
	result, _ := ioutil.ReadAll(r.Body)
	fmt.Fprintf(w, FetchtFlightInfo(result, "B"))

}

//FetchtFlightInfoForeAndBack 应该是综合了出发地和目的地这两个。通过传入的ForB 参数来定。如果获取出发地则为F，如果获取目的地则为B
//#TODO  FetchtFlightInfoForeAndBack 是ks_server的一个接口。这里综合了去程回程
func FetchtFlightInfoForeAndBack(w http.ResponseWriter, r *http.Request) {

	defer errorlog.DealRecoverLog()
	r.ParseForm()
	result, _ := ioutil.ReadAll(r.Body)
	//在这里将航班数据传入。后面为ALL；证明是去程回程一起搞
	//ks数据 从sqlserver 拿过来到110，接着110再分发到111，112，117三台
	go FetchtFlightInfo(result, "ALL")
	fmt.Fprintf(w, errorlog.Result(true))

}

//ks_server 多端服务(接受端),fares处理了fare包.
func ReloadDiscountCode(w http.ResponseWriter, r *http.Request) {
	defer errorlog.DealRecoverLog()

	r.ParseForm()
	result, _ := ioutil.ReadAll(r.Body)

	var err error

	defer func() {
		if err != nil {
			fmt.Fprint(w, errorlog.Result(false))
		} else {
			fmt.Fprint(w, errorlog.Result(true))
		}
	}()

	var ts mysqlop.TotleService

	if err = json.Unmarshal(result, &ts); err != nil {
		return
	}
	if ts.Deal == "Reload" {


		fmt.Println("更新折扣代码。从内存中删除对应的折扣代码，接着再从mysql加载最新并加载到内存")
		errorlog.WriteErrorLog("{{Syscall}} ReloadDiscountCode (1): " + ts.Code)


		//#TODO 这一步是根据传入的
		mysqlop.DCDelete(ts.Code)    //删除内存（删除对应的折扣代码）
		go mysqlop.DCSelect(ts.Code) //接着再从mysql中获取最新数据并加载数据到内存


	} else {
		err = errors.New("Err Param")
	}
}

func DeleteDiscountCode(w http.ResponseWriter, r *http.Request) {
	defer errorlog.DealRecoverLog()

	var err error

	defer func() {
		if err != nil {
			fmt.Fprint(w, errorlog.Result(false))
		} else {
			fmt.Fprint(w, errorlog.Result(true))
		}
	}()

	r.ParseForm()
	result, err := ioutil.ReadAll(r.Body)
	if err != nil {
		return
	}

	var ts mysqlop.TotleService
	if err = json.Unmarshal(result, &ts); err != nil {
		return
	}

	if ts.Deal == "Delete" {
		errorlog.WriteErrorLog("{{Syscall}} DeleteDiscountCode (1): " + ts.Code)
		mysqlop.DCDelete(ts.Code)
	} else {
		err = errors.New("Err Param")
	}
}




//这里是刷新数据源，从mysql里面拿最新的数据，接着再一步步更新到ks缓存里面去
func ReloadDataSource(w http.ResponseWriter, r *http.Request) {
	defer errorlog.DealRecoverLog()

	var err error

	defer func() {

		if err != nil {
			fmt.Fprint(w, errorlog.Result(false))
		} else {
			fmt.Fprint(w, errorlog.Result(true))
		}
	}()

	r.ParseForm()

	result, err := ioutil.ReadAll(r.Body)
	if err != nil {

		return
	}

	var ts mysqlop.TotleService
	if err = json.Unmarshal(result, &ts); err != nil {
		return
	}

	if ts.Deal == "Reload" {

		errorlog.WriteErrorLog("{{Syscall}} ReloadDataSource (1): ")

		//if Deal =="Reload" than，we should use mysqloto do sth
		go func() {

			mysqlop.DSKeyCacheSelect()
			mysqlop.DSDCSelect()
			mysqlop.AgencyGroupSelect()
			mysqlop.AgRoutineSelect()


		}()
	} else {
		err = errors.New("Err Param")
	}
}

func ReloadTax(w http.ResponseWriter, r *http.Request) {
	defer errorlog.DealRecoverLog()

	var err error

	defer func() {
		if err != nil {
			fmt.Fprint(w, errorlog.Result(false))
		} else {
			fmt.Fprint(w, errorlog.Result(true))
		}
	}()

	r.ParseForm()
	result, err := ioutil.ReadAll(r.Body)
	if err != nil {
		return
	}

	var ts mysqlop.TotleService
	if err = json.Unmarshal(result, &ts); err != nil {
		return
	}

	if ts.Deal == "Reload" {
		errorlog.WriteErrorLog("{{Syscall}} ReloadTax (1): " + ts.Code)

		go mysqlop.TaxSelect()
	} else {
		err = errors.New("Err Param")
	}
}

//航班信息查询（）
func WAPI_QueryFlightInfo_V1(w http.ResponseWriter, r *http.Request) {
	defer errorlog.DealRecoverLog()

	var err error
	flightinfo_output := &FlightInfo_Output{}
	flightinfo_output.Route = make([]*FlightInfo, 0, 5)

	defer func() {
		var flightinfo_out_json []byte
		if len(flightinfo_output.Route) > 0 {
			flightinfo_out_json = errorlog.Make_JSON_GZip(flightinfo_output)
		}

		if flightinfo_out_json == nil {
			fmt.Fprint(w, bytes.NewBuffer(FlightInfoErrOut))
		} else {
			fmt.Fprint(w, bytes.NewBuffer(flightinfo_out_json))
		}
	}()

	r.ParseForm()
	result, err := ioutil.ReadAll(r.Body)
	if err != nil {
		return
	}

	var fis []struct {
		DepartStation string   `json:"DepartStation"` //出发
		ArriveStation string   `json:"ArriveStation"` //目的地
		TravelDate    string   `json:"TravelDate"`    //旅行日期
		Airline       string   `json:"Airline"`       //航司
		FlightNumber  []string `json:"FlightNumber"`  //同条航线需要查询多个航班
		OnlyOAG       bool     `json:"OnlyOAG"`       //仅仅查OAG数据
		FlightNumber2 []string `json:"-"`             //航班号
		HaveData      []int    `json:"-"`
	}

	if err = json.Unmarshal(result, &fis); err != nil {
		return
	}

	totalline := 0
	for _, fi := range fis {
		if len(fi.DepartStation) != 3 || len(fi.ArriveStation) != 3 ||
			len(fi.TravelDate) != 10 || len(fi.Airline) != 2 {
			//err = errors.New("Param Error")
			continue
		}

		fi.FlightNumber2 = make([]string, 0, len(fi.FlightNumber))
		fi.HaveData = make([]int, len(fi.FlightNumber))
		for i, fn := range fi.FlightNumber {
			if len(fn) < 4 {
				fn = strings.Repeat("0", 4-len(fn)) + fn
				fi.FlightNumber[i] = fn
			}
			if len(fn)%4 != 0 || len(fn) > 8 {
				fi.FlightNumber2 = append(fi.FlightNumber2, "")
				continue
			}

			if len(fn) == 4 {
				fi.FlightNumber2 = append(fi.FlightNumber2, fn+fn) //直飞中转数据
			} else {
				fi.FlightNumber2 = append(fi.FlightNumber2, "")
			}
		}

		c_fjson := make(chan *cacheflight.FlightJSON, 1)
		QueryLegInfo(&cacheflight.RoutineService{
			Deal:           "QueryFore",
			DepartStation:  fi.DepartStation,
			ConnectStation: []string{},
			ArriveStation:  fi.ArriveStation,
			TravelDate:     fi.TravelDate},
			cachestation.PetchIP[fi.DepartStation], c_fjson)

		fore := <-c_fjson
		sort.Sort(fore.Route)

		for _, rout := range fore.Route {
			for i := range fi.FlightNumber {
				if fi.HaveData[i] == 0 &&
					(rout.FI[0].Legs[0].P == "OAG" || !fi.OnlyOAG && rout.FI[0].Legs[0].P == "1E") &&
					(rout.RFN == fi.FlightNumber[i] || rout.RFN == fi.FlightNumber2[i]) &&
					(rout.FI[0].Legs[0].OA == fi.Airline || rout.FI[0].Legs[0].AD == fi.Airline) {

					fi.HaveData[i] = 1
					flightinfo_output.Route = append(flightinfo_output.Route, Copy2FlightInfo("1", rout.DR, rout))
				}
			}
			if len(flightinfo_output.Route) == len(fi.FlightNumber)+totalline {
				break
			}
		}
		totalline = len(flightinfo_output.Route)
	}
}

//航班线路查询
// 航班时刻查询
//#TODO WAPI_QueryRoutine_V1
func WAPI_QueryRoutine_V1(w http.ResponseWriter, r *http.Request) {
	defer errorlog.DealRecoverLog()

	var err error
	flightinfo_output := &FlightInfo_Output{}
	flightinfo_output.Route = make([]*FlightInfo, 0, 5)

	//数据都处理完成后，在这里整合后return出去
	defer func() {

		var flightinfo_out_json []byte
		if len(flightinfo_output.Route) > 0 {
			flightinfo_out_json = errorlog.Make_JSON_GZip(flightinfo_output)
		}

		if flightinfo_out_json == nil {
			fmt.Fprint(w, bytes.NewBuffer(FlightInfoErrOut))
		} else {
			fmt.Fprint(w, bytes.NewBuffer(flightinfo_out_json))
		}
	}()

	r.ParseForm()
	result, err := ioutil.ReadAll(r.Body)
	if err != nil {
		return
	}

	var rs cacheflight.RoutineService

	/**
	  type RoutineService struct {
	  	Deal           string   `json:"Deal"`           //相当于一个type。which type do you li  //
	  	DepartStation  string   `json:"DepartStation"`  //出发
	  	ConnectStation []string `json:"ConnectStation"` //中转地
	  	ArriveStation  string   `json:"ArriveStation"`  //目的地
	  	TravelDate     string   `json:"TravelDate"`     //出发时间  2018-09-03
	  	Routine        string   `json:"Routine"`        //航线
	  	Legs           int      `json:"Legs"`           //   查一段，还是查2段
	  	Days           int      `json:"Days"`           // 天数 日期，日历，十天，二十天。
	  	Quick          bool     `json:"Quick"`          //?  快速。看本地有没有，如果有，直接在本地获取
	  }
	*/

	if err = json.Unmarshal(result, &rs); err != nil {
		return
	}

	//#TODO 这里必须强制传Deal == "QueryRoutine"
	if rs.Deal != "QueryRoutine" || len(rs.Routine) < 10 || (len(rs.Routine)-3)%7 != 0 ||
		len(rs.DepartStation) != 3 || len(rs.ArriveStation) != 3 || len(rs.TravelDate) != 10 {
		err = errors.New("No QueryRoutine")
		return
	}

	c_fjson := make(chan *cacheflight.FlightJSON, 1)
	//c_fjson就是回调，在函数的下一级会将数据返回来。在这里接收。就可以看到数据了
	QueryLegInfo(&rs, cachestation.PetchIP[rs.DepartStation], c_fjson)

	fore := <-c_fjson
	sort.Sort(fore.Route)

	for _, rout := range fore.Route {
		flightinfo_output.Route = append(flightinfo_output.Route, Copy2FlightInfo("1", rout.DR, rout))
	}
}
