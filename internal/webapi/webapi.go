package webapi

/****************************
存在思考问题:
(二):出发地与目的地的多行段接驳流程,是否有意义?
	1:至少必须控制接驳航线的输出量
	2:接驳功能只能在非Fare业务中实现,与Fare业务合并太过复杂难理清.

应该做的更好的:
(一):远程获取不到信息的(len==0)的航线应该支持查询出来.
(二):应该支持查询内存

****************************/
import (
	//"bytes"
	"cacheflight"
	"cachestation"
	//"compress/gzip"
	//"encoding/json"
	"errorlog"
	//"fmt"
	//"runtime"
	//"io/ioutil"
	"mysqlop"
	//"net/http"
	//"errors"
	"fmt"
	"outsideapi"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unsafe"
)

//Point2Point_Flight/Point2Point_In/mysqlop.Routine是传入参数结构
type Point2Point_Flight struct {
	DepartStation  string `json:"DepartStation"`  //出发地机场  XIY
	ConnectStation string `json:"ConnectStation"` //中转地机场  CAN   //wudy发过来的入口参数很多是没有传入这个
	ArriveStation  string `json:"ArriveStation"`  //目的地机场  JHB
	TravelDate     string `json:"TravelDate"`     //旅行日期  2018-09-08

}

//此结构不但用于航班shopping,也用于缓存查询（点到点输入参数）（重要重要！！！！）
//#TODO shopping 参数入口
type Point2Point_In struct {

	//2018.07.23 新增。wudy新需求。两人同行
	//AdultQty int `json:"AdultQty"`  //大人人数
	//ChildQty int `json:"ChildQty"`  //小孩人数
	Offices     []string              `json:"Offices"`     //注册公司？3007F=广州中航服商务管理有限公司
	Airline     []string              `json:"Airline"`     //航司
	Alliance    string                `json:"Alliance"`    //联盟名称
	Flight      []*Point2Point_Flight `json:"Flight"`      //一些出发信息，目的地信息
	ConnMinutes int                   `json:"ConnMinutes"` //中转总时间,单位分钟(默认为"300")
	MaxOutput   int                   `json:"MaxOutput"`
	//Customer      []string              `json:"Customer"`      //大客户代码(暂时请输入"")
	DeviationRate int    `json:"DeviationRate"` //绕航率(默认"150")
	BackFlag      int    `json:"BackFlag"`      //退票标示 2  代表什么
	ChangeFlag    int    `json:"ChangeFlag"`    // 改期标示  2
	BaggageFlag   int    `json:"BaggageFlag"`   //行李标示  2
	UserKind1     string `json:"UserKind1"`     //  用户类型1  （以前都是用这个，现在这两个都两用）
	UserKind2     string `json:"UserKind2"`     //  用户类型2  通过这类型去过滤用的  后来这两个参数没有用
	BerthType     string `json:"BerthType"`     // LCY 查找的舱位类型
	GetCount      int    `json:"GetCount"`      //获取第N次的数据(1 Or 2) （我们如果有100个供应商，我们要获取的是第几次，默认1）
	Debug         bool   `json:"Debug"`         //切入调试模式,输出较完整内容2016-04-01
	Quick         bool   `json:"Quick"`         // 快速模式（）
	ShowBCC       bool   `json:"ShowBCC"`       //是否显示Customer有航班无舱位信息（是否显示大客户的信息，可能不是最便宜的。比如我们专门卖南航，我们要显示出来）
	Deal          string `json:"Deal"`
	/*
		Deal="QueryDepartDays"
		Deal="QueryArriveDays"
		Deal="QueryDepartLegsDays"
		Deal="QueryArriveLegsDays"
	*/
	Legs    int                `json:"Legs"`    //fare查询参数 返回多少个航段
	Days    int                `json:"Days"`    //fare查询参数  获取几天的数据
	TheTrip bool               `json:"TheTrip"` //标识是否只是获取双程票单(有时候单程也会返回)
	Rout    []*mysqlop.Routine `json:"Rout"`    //这里是多票单合并,直接传输中转地,如不这里,分析后的中转地会是错误的  （航班真实的一个航班情况。上面的没有标示第几段）

	/**
		mysqlop.Routine 就是下面这个样式。可以通过
		{
		DepartStation string   `json:"DS"`
		DepartCounty  []string `json:"DC"`
		ArriveStation string   `json:"AS"`
		ArriveCounty  []string `json:"AC"`
		TravelDate    string   `json:"TD"`
		Wkday         int      `json:"WD"`
		DayDiff       int      `json:"DD"`
		Segment       int      `json:"SC"`
		Trip          int      `json:"T"`
		GoorBack      int      `json:"GB"`
		CrossSeason   string   `json:"CS"` //这里指的是记录是否跨季 A(不跨) B(跨)
		Stay          int      `json:"S"`  //双程停留天数
	}
	*/
	//2017-11-29测试暂时参数
	Agency string `json:"Agency"` //ShowShoppingCode/DataSourceCode  （测试用的。比如特定指定返回某个供应商的数据）

}

//#TODO  Result 这里有下面4个返回
//Point2Point_Output是Shopping输出结构
type Point2Point_Output struct {
	Result  int                                `json:"Result"` //结果情况 (1)正常结束 (2)正常结束&可以获取下一(更多)次数据  (3)超时 (4)错误 (100)MidJourney=NULL
	Segment []*Point2Point_RoutineInfo2Segment `json:"Segment"`
	Fare    []*FareInfo                        `json:"Fare"`
	Journey JourneyInfoList                    `json:"Journey"`
	//Debuginfo []*DebugInfo                       `json:"Debug"`
}

//静态票单数据归类的结构 (压缩票单的航线,可以减少处理的循环)
type MiddleMatch struct { //保存票价和航班匹配的结果(一条动态航班动态航班被多条票价对应到)
	doneID []int                            //MainBill.ID那些已经shopping的ID集(某一航班适合的多条Fare记录)，这里主要是用于Shopping节点输出,中间没有计算过了
	output JMIList                          //那些已经shopping的ID的Shopping半结果集(Journey使用某一航班&Fare的情况)
	fjson  *Point2Point_RoutineInfo2Segment //Flight&SC某一航班的信息{SegemntCode,FlightJSON},这里主要是用于Shopping节点输出,中间没有计算过了
}

type MB2RouteInfo struct {
	Routine    string //是包含中转城市的(多中转多航司)
	Segment    int
	TravelDate string
	Trip       int    //MainBill存在单双程混合情况,这里是单/双程参数.
	Agency     string //GDS,这里的Agency是通过票单的前续判断Fare,通过Fare.Agency获取
	PCC        string
	ListMB     mysqlop.ListMainBill

	//shopping参数,这里是因为一条RI记录使用多个动态航班
	MM    []*MiddleMatch //保存票价和航班匹配的结果
	MMBCC []*MiddleMatch //保存票价和航班匹配的结果 20170526 BCC

	noget      bool //没数据,都不应该远程获取.情况有:航班绕航率问题,当天没飞问题
	MatchCount int  //进入MainBill2Legs匹配的次数;当noget=false & MatchCount=0 & OAG=true是才取远程获取数据;防止有航班无舱位还去获取
	matched    bool //是否成功配对,非成功配对的必须再次处理双程中2个单程组合问题

	//多中转多航司的参数,(20170526：其实Routine已经分开了,中转地的不同只能通过MB.ID去判断)
	diffRoutine bool //存在异同路径,这时候应该过滤MixBerth=(1,3)及小飞飞
	sameRoutine bool //存在相同路径,高优先级,异同路径如果价格没有低,就没有意义
	//prefixDS    bool //存在电商数据(这里被af代替了)
	//AgencyFrame
	af *cachestation.Agencies //af里面的Agency和上面的Agency是不同的
}

/*************以下是Shopping输出结构*************/
//Point2Point_RoutineInfo2Segment是服务端接受及输出的FlightJSON结构
type Point2Point_RoutineInfo2Segment struct {
	SC string `json:"SC"` //例如"FB_0_1_11"
	*cacheflight.RoutineInfoStruct
}

//Shopping输出的票单字段,其中FC=ID,用于输出的票单信息部分
type FareInfo struct {
	FareCode string `json:"FC"`
	*mysqlop.MainBill
	/*2016-03-17
	这里加入配置系统的信息,因为配置系统是动态的数据,没办法一开是绑定Fare
	(1):Fare在动态环境下是没办法知道属于单层/双程,或者某航线的一段
	(2):Journey可以指定DiscountCode内容,但冗余太大.破坏Journey本身的描述信息
	*/
}

//用于匹配Journey的中间结果,用于业务接驳
type JourneyMiddleInfo struct {
	SegmentCode  string
	FareCode     string
	CMs          int               //加入这个是为了结果的中转时间排列()
	CI           []*CabinsInfo     //这里获取的是航班的部分信息,必要时可以引入航班整个记录
	Mb           *mysqlop.MainBill //主要是为了以后各方面的条件控制(不同路线来回程控制)
	Routine      string
	FlightNumber string
	//Lower        bool //用户区别是最低价格或者是小飞飞数据
	Status int //Journey状态,(0)成功 (1)一程无舱位 (2)双程无舱位

	//20170526
	MM *MiddleMatch //这里是为了排列价格用的

	afAgency *cachestation.Agencies //记录供应商平台
}

type JMIList []*JourneyMiddleInfo

func (this JMIList) Len() int {
	return len(this)
}

func (this JMIList) Less(i, j int) bool {
	return this[i].Mb.AdultsPrice <= this[j].Mb.AdultsPrice
}

func (this JMIList) Swap(i, j int) {
	this[i], this[j] = this[j], this[i]
}

//Shopping.Journey中的舱位使用信息
type CabinsInfo struct {
	Cabin        string `json:"Cabin"`        //票单舱位级别
	FlightCOS    string `json:"FlightCOS"`    //舱位代码
	Availability string `json:"Availability"` //可预订数
}

//Shopping.Journey中的某Segment详细使用的航班及票单
type JourneySegmentInfo struct {
	SC     string        `json:"SC"`     //Segment Code
	FC     string        `json:"FC"`     //Fare Code
	TID    string        `json:"TID"`    //TicketID Modified 2016-06-06
	NO     string        `json:"NO"`     //Order NO
	Cabins []*CabinsInfo `json:"Cabins"` //Using Cabin Info
}

//Journey中的价格情况
type TicketPriceInfo struct {
	PassengerType       string `json:"PassengerType"`
	Currency            string `json:"Currency"`
	BasePrice           int    `json:"BasePrice"`
	Tax                 string `json:"Tax"`
	TotalPrice          int    `json:"TotalPrice"`
	PriceBreakdown      string `json:"PriceBreakdown"`
	TaxAgency           string `json:"TaxAgency"`
	LastTaxDate         string `json:"LastTaxDate"`
	fareCalculation     string `json:"fareCalculation"`
	latestTicketingTime string `json:"latestTicketingTime"`
	SourceBasePrice     int    `json:"SourceBasePrice"`
	SourceTotalPrice    int    `json:"SourceTotalPrice"`
	AgencyFee           string `json:"AgencyFee"`
	EncourageFee        string `json:"EncourageFee"`
	TicketFee           string `json:"TicketFee"`
}

//Shopping.Journey.OfficeID中的TicketAgencyInfo的出票情况
type JourneyTicketInfo struct {
	TicketId         string              //出票序列号
	MarketingAirline string              //出票航司Fare.OA
	TicketAgency     []*TicketAgencyInfo //代理商,出票代理是多注册公司对应多供应商的
}

//Shopping.Journey.OfficeID中的某Ticket详细使用的代理人及价格
type TicketAgencyInfo struct {
	Agency        string             `json:"Agency"`
	AgencyRay     string             `json:"AgencyRay"`
	OfficeId      string             `json:"OfficeId"`
	SessionID     string             `json:"Data"`
	TicketPricing []*TicketPriceInfo `json:"TicketPricing"`
}

//Journey
type JourneyInfo struct {
	Customer   string `json:"Customer"` //大客户代码
	Status     int    `json:"Status"`   //Journey状态,(0)成功 (1)一程无舱位 (2)双程无舱位
	Currency   string `json:"Currency"`
	BasePrice  int    `json:"BasePrice"`  //基本价格
	Tax        string `json:"Tax"`        //税费
	TotalPrice int    `json:"TotalPrice"` //总价格
	//FQKey             string                `json:"FQKey"`
	SourceBasePrice  int    `json:"SourceBasePrice"`
	SourceTotalPrice int    `json:"SourceTotalPrice"`
	Bestbuy          string `json:"Bestbuy"`
	//RebatePercent     string                `json:"RebatePercent"`
	//RebateAdd         string                `json:"RebateAdd"`
	//SpecialBasePrice  string                `json:"SpecialBasePrice"`
	//SpecialTotalPrice string                `json:"SpecialTotalPrice"`
	JourneySegment []*JourneySegmentInfo `json:"JourneySegment"`
	JourneyTicket  []*JourneyTicketInfo  `json:"JourneyTicket"`
	Mid_journey    *JourneyMiddleInfo    `json:"-"` //为了更复杂的匹配
	Routine        string                `json:"-"` //价格去重
	FlightNumber   string                `json:"-"` //价格去重
}

type JourneyInfoList []*JourneyInfo //排列价格专用

/***************Cache Shopping Start************/

//#TODO 这是最终要传出去的
type ShoppingResult struct {
	InsertTime time.Time
	Shopping   []byte //gzip压缩后结果
}

var SR1 map[string]*ShoppingResult

var SR1Lock sync.RWMutex

var Airline1E map[string]bool
var nullFlightJSON = &cacheflight.FlightJSON{}
var errShopping = &Point2Point_Output{
	Result:  4,
	Segment: []*Point2Point_RoutineInfo2Segment{},
	Fare:    []*FareInfo{}}

var ShoppingErrOut []byte
var ShoppingTimeOut []byte
var ShoppingNULL []byte
var ShoppingNULLResult2 []byte
var ListShoppingEnd []byte
var ListShoppingErr []byte

var prefix_DS = "DS-"        //电商前缀
var prefix_FareV2 = "FareV2" //FareV2前缀

/***************Cache Shopping End**************/
func init() {

	//缓存的Shopping量为20000次/5分钟(SR1为不缓存结果)
	SR1 = make(map[string]*ShoppingResult, 1000)
	ListShoppingErr = errorlog.Make_JSON_GZip(&ListPoint2Point_Output{ListShopping: []*Point2Point_Output{{4, []*Point2Point_RoutineInfo2Segment{}, []*FareInfo{}, []*JourneyInfo{}}}})
	ListShoppingEnd = errorlog.Make_JSON_GZip(&ListPoint2Point_Output{ListShopping: []*Point2Point_Output{{1, []*Point2Point_RoutineInfo2Segment{}, []*FareInfo{}, []*JourneyInfo{}}}})
	ShoppingErrOut = errorlog.Make_JSON_GZip(&Point2Point_Output{4, []*Point2Point_RoutineInfo2Segment{}, []*FareInfo{}, []*JourneyInfo{}})
	ShoppingTimeOut = errorlog.Make_JSON_GZip(&Point2Point_Output{3, []*Point2Point_RoutineInfo2Segment{}, []*FareInfo{}, []*JourneyInfo{}})
	ShoppingNULLResult2 = errorlog.Make_JSON_GZip(&Point2Point_Output{2, []*Point2Point_RoutineInfo2Segment{}, []*FareInfo{}, []*JourneyInfo{}})
	ShoppingNULL = errorlog.Make_JSON_GZip(&Point2Point_Output{1, []*Point2Point_RoutineInfo2Segment{}, []*FareInfo{}, []*JourneyInfo{}})
	FlightInfoErrOut = errorlog.Make_JSON_GZip(&FlightInfo_Output{[]*FlightInfo{}})
	DiscountCodeErrOut = errorlog.Make_JSON_GZip(&DiscountCode_Output{[]*DC_Model_3{}, []*SegDiscountCode{}, []*SegDiscountCode{}})

	//1E里面对应的各个航司
	Airline1E = map[string]bool{
		"CA": true,
		"CZ": true,
		"MU": true,
		"HU": true,
		"FM": true,
		"ZH": true,
		"MF": true,
		"HX": true,
		"UO": true,
		"HO": true,
		"NX": true,
		"SC": true,
		"3U": true,
		"VN": true,
		"EK": true,
		"KE": true,
		"OZ": true,
		"EY": true,
		"QR": true,
		"TG": true,
		"KQ": true,
		"5J": true,
		"PR": true,
		"GA": true,
		"3A": true,
		"CX": true,
		"KA": true}

	ConnectStationCache.ConnectStation = make(map[string][]string, 50000)
}

//结构的函数
func (this *JourneySegmentInfo) Copy() *JourneySegmentInfo {
	canins := this.Cabins
	jsi := *this
	jsi.Cabins = make([]*CabinsInfo, 0, len(canins))

	for _, ci := range canins {
		c := *ci
		jsi.Cabins = append(jsi.Cabins, &c)
	}
	return &jsi
}

func (this *JourneyTicketInfo) Copy() *JourneyTicketInfo {
	qtai := this.TicketAgency
	jti := *this
	jti.TicketAgency = make([]*TicketAgencyInfo, 0, len(qtai))
	for _, tai := range qtai {
		ta := *tai
		ta.TicketPricing = make([]*TicketPriceInfo, 0, 2)
		tpi := tai.TicketPricing
		for _, tp := range tpi {
			t := *tp
			ta.TicketPricing = append(ta.TicketPricing, &t)
		}
		jti.TicketAgency = append(jti.TicketAgency, &ta)
	}
	return &jti
}

func (this JourneyInfoList) Len() int {
	return len(this)
}

func (this JourneyInfoList) Less(i, j int) bool {
	leni := len(this[i].Mid_journey.Routine)
	lenj := len(this[j].Mid_journey.Routine)

	//长度不一样的情况下，则按照直飞返回出去。
	if leni != lenj { //返回直飞的优先
		return leni < lenj
	} else {

		//按照低价返回出去
		if this[i].TotalPrice != this[j].TotalPrice {
			return this[i].TotalPrice < this[j].TotalPrice
		} else {
			//按照中转时间低的返回出去
			return this[i].Mid_journey.CMs <= this[j].Mid_journey.CMs
		}
	}
}

func (this JourneyInfoList) Swap(i, j int) {
	this[i], this[j] = this[j], this[i]
}

/*************以上是Shopping输出结构*************/
//来回程的判定
/**


//传入的参数
type Point2Point_Flight struct {
	DepartStation  string `json:"DepartStation"`   //出发地机场  XIY
	ConnectStation string `json:"ConnectStation"` //中转地机场  CAN   //wudy发过来的入口参数很多是没有传入这个
	ArriveStation  string `json:"ArriveStation"` //目的地机场  JHB
	TravelDate     string `json:"TravelDate"`  //旅行日期  2018-09-08
}


//输出航班信息
type Routine struct {
	DepartStation string   `json:"DS"`
	DepartCounty  []string `json:"DC"`
	ArriveStation string   `json:"AS"`
	ArriveCounty  []string `json:"AC"`
	TravelDate    string   `json:"TD"`
	Wkday         int      `json:"WD"`
	DayDiff       int      `json:"DD"`
	Segment       int      `json:"SC"`
	Trip          int      `json:"T"`
	GoorBack      int      `json:"GB"`
	CrossSeason   string   `json:"CS"` //这里指的是记录是否跨季 A(不跨) B(跨)
	Stay          int      `json:"S"`  //双程停留天数
}


*/
//将航班的一些信息转为Routine
func FlightLegs2Routine(legs []*Point2Point_Flight) (rout []*mysqlop.Routine) {

	var (
		oldarrive    string
		olddate      string
		segment      int
		date         time.Time
		wkday        int
		daydiff      int      //这几天不要的
		season       []int    //跨季度计算
		departcounty []string //出发国家
		arrivecounty []string //到达国家
		ok           bool
	)

	today, _ := time.Parse("2006-01-02", errorlog.Today()) //今天

	//传入有几段
	season = make([]int, 0, len(legs))

	for _, leg := range legs {

		if oldarrive != leg.DepartStation || olddate != leg.TravelDate {
			segment++
		}

		//将这段leg的出发机场和到达机场分别赋值给oldarrive和olddate
		oldarrive = leg.ArriveStation
		olddate = leg.TravelDate

		date, _ = time.Parse("2006-01-02", leg.TravelDate)                  //出发日趋
		wkday = errorlog.GetWeekDay(date.Weekday().String())                //今天是周几
		daydiff = int(date.Sub(today).Hours() / 24)                         //出发日期距离今天还要多少天
		mon, _ := strconv.Atoi(errorlog.GetMonthNum(date.Month().String())) //月份
		season = append(season, mon/3)                                      //通过月份，来判断是第几季度。

		if departcounty, ok = cachestation.CityCounty[leg.DepartStation]; !ok {
			departcounty = []string{leg.DepartStation}
		}

		if arrivecounty, ok = cachestation.CityCounty[leg.ArriveStation]; !ok {
			arrivecounty = []string{leg.ArriveStation}
		}

		r := &mysqlop.Routine{
			DepartStation: leg.DepartStation,
			DepartCounty:  departcounty,
			ArriveStation: leg.ArriveStation,
			ArriveCounty:  arrivecounty,
			TravelDate:    leg.TravelDate,
			Wkday:         wkday,
			DayDiff:       daydiff, //距离今天出发还有几天
			Segment:       segment, //第几段
			Trip:          0,
			GoorBack:      0,
			CrossSeason:   "A"}

		//最终将routine拼接到rout  这个大切片里面去。如果空，则新建一个，并添加到里面去；否则的话，则直接拼接
		if rout == nil {
			rout = []*mysqlop.Routine{r}
		} else {
			rout = append(rout, r)
		}
	}

	//这里如果是特殊的来回程(PS: PEK-HKG/HKG-SHA),方法的处理会是错误的。入如果行程是这样的话，则是开口程
	//开口程的来回程这样判断是错误的
	len := len(rout)
	for i := 0; i < len; i++ {
		for j := i + 1; j < len; j++ {

			//如果上一段的出发===下一段的到达  && 如果上一段的到达===下一段的出发  &&  上一段的日期早于下一段
			if rout[i].DepartStation == rout[j].ArriveStation &&
				rout[i].ArriveStation == rout[j].DepartStation &&
				rout[i].DayDiff <= rout[j].DayDiff {

				rout[i].Trip = 1
				rout[i].GoorBack = 0
				rout[j].Trip = 1
				rout[j].GoorBack = 1
				if season[i] == season[j] {
					rout[i].CrossSeason = "A"
					rout[j].CrossSeason = "A"
				} else {
					rout[i].CrossSeason = "B"
					rout[j].CrossSeason = "B"
				}
				rout[i].Stay = rout[j].DayDiff - rout[i].DayDiff //在该地呆几天
				rout[j].Stay = rout[j].DayDiff - rout[i].DayDiff
			}
		}
	}

	//CAN [CAN] BKK [BKK] 2018-09-08 6 47 1 0 0 A 0}

	return
}

//QueryLegInfo_V2主要用于Shopping,特点是航班要排序,但相同航班不可以过滤,因为可能舱位不同.
func QueryLegInfo_V2(rs *cacheflight.RoutineService, serverIP string, cfjs chan *cacheflight.FlightJSON) {
	c_fjson := make(chan *cacheflight.FlightJSON, 1)
	QueryLegInfo(rs, serverIP, c_fjson)
	fore := <-c_fjson
	//把Routine中的AD换成OA,这时Shopping的特殊需求
	for _, rout := range fore.Route {
		for k, v := range rout.FI[0].Legs {
			if k == 0 {
				if v.OA == "" {
					rout.R = v.DS + "-" + v.AD + "-" + v.AS
				} else {
					rout.R = v.DS + "-" + v.OA + "-" + v.AS
				}
				if v.PCC == "" {
					v.PCC = "CAN131"
				}
			} else {
				if v.OA == "" {
					rout.R += "-" + v.AD + "-" + v.AS
				} else {
					rout.R += "-" + v.OA + "-" + v.AS
				}
			}
		}
	}

	//sort.Sort(fore.Route)在后面分派到某天时有排序
	cfjs <- fore
}

//静态票单数据归类缩小
func MainBill2RouteInfo(
	af *cachestation.Agencies, //供应商平台
	rmb mysqlop.ResultMainBill, //静态Fare
	rout []*mysqlop.Routine) ( //查询条件
	mbris []*MB2RouteInfo) {

	mbris = make([]*MB2RouteInfo, 0, 50)

	getMutilRoutine := func(routesMutil string) []string {
		if len(routesMutil) <= 10 {
			return []string{routesMutil}
		}

		rets := []string{}

		for _, routes := range strings.Split(routesMutil, "$") {
			srout := strings.Split(routes, "-")
			if len(srout) == 3 {
				for _, airline := range strings.Split(srout[1], " ") {
					rets = append(rets, srout[0]+"-"+airline+"-"+srout[2])
				}
			} else if len(srout) == 5 { //这里缺少处理多个中转的情况,他们是适用空格分隔
				for _, conn := range strings.Split(srout[2], " ") {
					for _, airline1 := range strings.Split(srout[1], " ") {
						for _, airline2 := range strings.Split(srout[3], " ") {
							rets = append(rets, srout[0]+"-"+airline1+"-"+conn+"-"+airline2+"-"+srout[4])
						}
					}
				}
			}
		}

		return rets
	}

	var (
		routine    string
		haddata    bool
		segment    int
		TravelDate string
	)

	FindCounty := func(countys []string, station string) bool {
		for _, county := range countys {
			if county == station {
				return true
			}
		}
		return false
	}

	for _, mb := range rmb.Mainbill {
		if mb.Agency != af.ShowShoppingName && !strings.HasPrefix(mb.BillID, af.BillPrefix) {
			continue //过滤掉不同供应商的数据
		}

		//routine = mb.Routine //mb.Routine可以是多个的KUL-KA CX-HKG-3A-ZYK
		for _, routine = range getMutilRoutine(mb.Routine) { //这里必须先适用$划分多条航线
			haddata = false

			for _, ri := range mbris {
				if routine == ri.Routine && mb.Trip == ri.Trip &&
					mb.Agency == ri.Agency && mb.PCC == ri.PCC {
					ri.ListMB = append(ri.ListMB, mb)
					haddata = true
					break
				}
			}

			if !haddata {
				segment = 0
				for _, r := range rout {
					if FindCounty(r.DepartCounty, mb.Springboard) &&
						FindCounty(r.ArriveCounty, mb.Destination) {
						segment = r.Segment
						TravelDate = r.TravelDate
						break
					}
				}

				if segment != 0 {
					mbris = append(mbris, &MB2RouteInfo{
						Routine:    routine,
						Segment:    segment,
						TravelDate: TravelDate,
						Trip:       mb.Trip,
						Agency:     mb.Agency,
						PCC:        mb.PCC,
						ListMB:     append(make([]*mysqlop.MainBill, 0, 20), mb),
						MM:         make([]*MiddleMatch, 0, 30),
						MMBCC:      make([]*MiddleMatch, 0, 10),
						af:         af,
					})
				}
			}
		}
	}
	return
}

//航班号匹配函数
func match(leg *cacheflight.FlightInfoStruct, ApplyAir string) bool {
	ab := false //当只有时间时使用(后面没航班要求)
	for _, aaN := range strings.Split(ApplyAir, " ") {
		if aaN == "*" || aaN == leg.OA || /*leg.OA == "" &&*/ aaN == leg.AD {
			return true
		}
		ab = false
		aaLen := len(aaN)

		if aaLen == 5 {
			if aaN[:2] == leg.AD && aaN[2:] == "(*)" {
				return true
			} else if aaN[0] == 'b' && aaN[1:] <= "2400" {
				dst := leg.DST[:2] + leg.DST[3:]
				if dst <= aaN[1:] {
					ab = true //return true适合时间还必须适合航班
					continue
				} else {
					return false
				}
			} else if aaN[0] == 'a' && aaN[1:] <= "2400" {
				dst := leg.DST[:2] + leg.DST[3:]
				if dst >= aaN[1:] {
					ab = true //return true适合时间还必须适合航班
					continue
				} else {
					return false
				}
			}
		}

		if aaLen < 6 {
			continue
		}

		si := 1
		if i := strings.IndexByte(aaN, ')'); i > 1 {
			si = i
		}

		if aaN[:2] == leg.AD {
			if aaN[2] == '(' && aaN[aaLen-1] == ')' {
				return true
			} else if aaN[2] != '(' && si > 1 {
				continue
			}
		} else if aaN[:2] != leg.AD && aaN[2] == '(' {
			if si < 4 {
				continue
			}

			has := false
			for _, oa := range strings.Split(aaN[3:si], "\\") {
				if oa == leg.OA || leg.OA == "" && oa == leg.AD || oa == "*" {
					has = true //这里不可以直接返回,因为后面可能有航班号
				}
			}

			if !has {
				continue
			}
		} else {
			continue
		}

		switch aaLen - si - 1 {
		case 0:
			return true
		case 4:
			if aaN[aaLen-4:] == leg.FN {
				return true
			}
		case 9:
			si = strings.IndexByte(aaN, '-')
			if aaLen-si != 5 {
				continue
			}
			if leg.FN >= aaN[si-4:si] && leg.FN <= aaN[si+1:] { //这里没转数字时不好的
				return true
			}
		default:
		}
	}

	return ab
}

//静态票单数据匹配动态航班数据(对比部分);以舱位为匹配标准,和机场无关联.
func MainBill2Legs(
	ri *MB2RouteInfo, //某航线静态票单信息
	fore_rout *cacheflight.RoutineInfoStruct, //某动态航班
	sc string, //Journey Segment Code
	cis [][]*cacheflight.CabinInfoStruct) ( //这里是舱位过滤的变量([Leg][Code])
	recis [][]*cacheflight.CabinInfoStruct, //这里是舱位过滤的变量([Leg][Code])
	add bool) { /*是否成功匹配*/

	ri.MatchCount++ //防止有航班无舱位还去远程获取数据

	mid := make([]int, 0, 10)
	output := make([]*JourneyMiddleInfo, 0, 10)
	legs := fore_rout.FI[0].Legs
	lenlegs := len(legs)
	partial := len(ri.Routine) != 17 || ri.Routine[4:6] == ri.Routine[11:13]

	if cis == nil {
		recis = make([][]*cacheflight.CabinInfoStruct, lenlegs, lenlegs) //这里是舱位过滤的变量(去除多余的航班舱位)
		for i := range recis {
			recis[i] = make([]*cacheflight.CabinInfoStruct, 0, len(ri.ListMB))
		}
	} else {
		recis = cis
	}
	for _, lv := range ri.ListMB {

		if lv.BigCustomerCode != "" {
			continue //BCC单独处理
		}

		//FareV2中航班限制条件中部分适合的情况处理(这里暂时是简单的处理)
		partialFit := func() bool {
			if len(lv.ApplyAir) < 6 || partial {
				return false
			}

			return true
		}

		lvBerth := strings.Split(lv.Berth, "/") //berth是多组成的U/Y

		for len(lvBerth) < lenlegs {
			lvBerth = append(lvBerth, lvBerth[0])
		}

		var ApplyAirs []string
		var NotFitApplyAirs []string

		if len(lv.ApplyAir) > 0 {
			ApplyAirs = strings.Split(lv.ApplyAir, "/") //航线航段分隔
		}

		if len(lv.NotFitApplyAir) > 0 {
			NotFitApplyAirs = strings.Split(lv.NotFitApplyAir, "/") //航线航段分隔
		}

		ci := make([]*CabinsInfo, 0, lenlegs) //这里是Journey中指定的舱位列表(销售时舱位)
		matchSucess := false                  //这个变量时为了FareV2中有时候航班限制条件是部分适合的情况.

		for m, leg := range legs {
			matchFlight := func() bool {
				Apply := ""
				NotApply := ""
				if len(ApplyAirs) > m {
					Apply = ApplyAirs[m]
				} else if len(ApplyAirs) > 0 {
					Apply = ApplyAirs[0]
				}
				if len(NotFitApplyAirs) > m {
					NotApply = NotFitApplyAirs[m]
				} else if len(NotFitApplyAirs) > 0 {
					NotApply = NotFitApplyAirs[0]
				}

				//r := (Apply == "" || match(leg, Apply)) && (NotApply == "" || !match(leg, NotApply))
				//if !r {
				//	fmt.Println("Enter matchFlight", Apply, "#", NotApply, "#", leg.OA, "#", leg.AD, "#", leg.FN, Apply == "" || match(leg, Apply), NotApply == "" || !match(leg, NotApply), fore_rout.R, lv.Berth) ///////////////
				//}
				return (Apply == "" || match(leg, Apply)) && (NotApply == "" || !match(leg, NotApply))
			}

			lvNext := false //第1段都失败了,第2段就不用再进行了
			fCI := leg.CI
			for _, fci := range fCI {
				if fci != nil && lvBerth[m] == fci.C { //这里曾经出现以个nil address错误(可能是版本beta问题,也可能是新获取航班问题)
					if fci.N > "0" && fci.N <= "9" || fci.N == "A" {
						if matchFlight() {
							matchSucess = true
						} else {
							if !strings.HasPrefix(lv.BillID, prefix_FareV2) || !partialFit() {
								lvNext = true
								break
							}
						}
						hasdata := false //该舱位已经加入shopping数据,recis加入操作.
						for _, cv := range recis[m] {
							if cv.C == fci.C {
								hasdata = true
								break
							}
						}
						if !hasdata {
							recis[m] = append(recis[m], fci)
						}

						ci = append(ci, &CabinsInfo{ //主要操作,加入Shopping节点的信息.
							Cabin:        lv.BillBerth,
							FlightCOS:    lvBerth[m], //lv.Berth,
							Availability: fci.N})
					}
					break
				}
			}

			if lvNext {
				break
			}
		}

		if len(ci) == lenlegs && matchSucess { //确保每一航段都存在该舱位代码,如果是,那么该舱位适合Journey.
			mid = append(mid, lv.ID)
			output = append(output, &JourneyMiddleInfo{
				SegmentCode:  sc,
				FareCode:     strconv.Itoa(lv.ID),
				CMs:          fore_rout.CMs,
				CI:           ci,         //[]*CabinsInfo
				Mb:           lv,         //MB,匹配不同路线来回程
				Routine:      ri.Routine, //fore_rout.R,(ri.Routine可以解决无航班的BCC问题)
				FlightNumber: fore_rout.RFN,
				afAgency:     ri.af})
		}
	} //舱位的匹配完成

	if len(mid) > 0 {
		ri.MM = append(ri.MM, &MiddleMatch{
			doneID: mid,
			output: output,
			fjson:  &Point2Point_RoutineInfo2Segment{sc, fore_rout},
		})
		return recis, true
	} else {
		return cis, false
	}
}

//BCC匹配航班,规则与MainBill2Legs不同,不求有航班舱位,只求最低价格
func MainBill2LegsBCC(
	ri *MB2RouteInfo, //某航线静态票单信息
	fore_rout *cacheflight.RoutineInfoStruct, //某动态航班
	sc string, //Journey Segment Code
	cis [][]*cacheflight.CabinInfoStruct, //这里是舱位过滤的变量([Leg][Code])
	ShowBCC bool) (
	recis [][]*cacheflight.CabinInfoStruct, //这里是舱位过滤的变量([Leg][Code])
	add bool) { /*是否成功匹配*/

	//BCC与到远程获取的关系是:是否匹配到和出Shopping什么价,没关系.
	ri.MatchCount++ //防止有航班无舱位还去远程获取数据

	mid := make([]int, 0, 10)
	output := make([]*JourneyMiddleInfo, 0, 10)
	legs := fore_rout.FI[0].Legs
	lenlegs := len(legs)

	if cis == nil {
		recis = make([][]*cacheflight.CabinInfoStruct, lenlegs, lenlegs) //这里是舱位过滤的变量(去除多余的航班舱位)
		for i := range recis {
			recis[i] = make([]*cacheflight.CabinInfoStruct, 0, len(ri.ListMB))
		}
	} else {
		recis = cis
	}

	for _, lv := range ri.ListMB {

		if lv.BigCustomerCode == "" {
			continue
		}

		lvBerth := strings.Split(lv.Berth, "/") //berth是多组成的U/Y

		for len(lvBerth) < lenlegs {
			lvBerth = append(lvBerth, lvBerth[0])
		}

		var ApplyAirs []string
		var NotFitApplyAirs []string

		if len(lv.ApplyAir) > 0 {
			ApplyAirs = strings.Split(lv.ApplyAir, "/") //航线航段分隔
		}

		if len(lv.NotFitApplyAir) > 0 {
			NotFitApplyAirs = strings.Split(lv.NotFitApplyAir, "/") //航线航段分隔
		}

		status := 0                           //是否存在未完成的舱位
		ci := make([]*CabinsInfo, 0, lenlegs) //这里是Journey中指定的舱位列表(销售时舱位)
		for m, leg := range legs {

			matchFlight := func() bool {
				Apply := ""
				NotApply := ""
				if len(lv.ApplyAir) > m {
					Apply = ApplyAirs[m]
				} else if len(lv.ApplyAir) > 0 {
					Apply = ApplyAirs[0]
				}
				if len(NotFitApplyAirs) > m {
					NotApply = NotFitApplyAirs[m]
				} else if len(NotFitApplyAirs) > 0 {
					NotApply = NotFitApplyAirs[0]
				}

				return (Apply == "" || match(leg, Apply)) && (NotApply == "" || !match(leg, NotApply))
			}

			doneCabin := false //已完成舱位匹配
			fCI := leg.CI
			for _, fci := range fCI {
				if fci != nil && lvBerth[m] == fci.C {
					if matchFlight() {

						hasdata := false //该舱位已经加入shopping数据
						for _, cv := range recis[m] {
							if cv.C == fci.C {
								hasdata = true
								break
							}
						}
						if !hasdata {
							recis[m] = append(recis[m], fci)
						}

						doneCabin = true
						ci = append(ci, &CabinsInfo{
							Cabin:        lv.BillBerth,
							FlightCOS:    lvBerth[m],
							Availability: fci.N})
						//这里又一个不适合场景:但fci.N不是有效座位数时,必须根据ShowBCC处理
					}
					break
				}
			}

			if ShowBCC && !doneCabin && matchFlight() { //所有舱位匹配不到
				ci = append(ci, &CabinsInfo{
					Cabin:        lv.BillBerth,
					FlightCOS:    lvBerth[m],
					Availability: "0"})
				status = 1
			}
		}

		if len(ci) == lenlegs { //确保每一航段都存在该舱位代码,如果是,那么该舱位适合Journey.
			mid = append(mid, lv.ID)
			output = append(output, &JourneyMiddleInfo{
				SegmentCode:  sc,
				FareCode:     strconv.Itoa(lv.ID),
				CMs:          fore_rout.CMs,
				CI:           ci,         //[]*CabinsInfo
				Mb:           lv,         //MB,匹配不同路线来回程
				Routine:      ri.Routine, //fore_rout.R,(ri.Routine可以解决无航班的BCC问题)
				FlightNumber: fore_rout.RFN,
				Status:       status,
				afAgency:     ri.af})
		}
	} //舱位的匹配完成

	if len(mid) > 0 {
		mm := &MiddleMatch{
			doneID: mid,
			output: output,
			fjson:  &Point2Point_RoutineInfo2Segment{sc, fore_rout},
		}
		for _, out := range output {
			out.MM = mm
		}
		ri.MMBCC = append(ri.MMBCC, mm)
		return recis, true
	} else {
		return cis, false
	}
}

//静态票单数据匹配动态航班数据(流程控制)
func MainBillMatchingLegs(
	af *cachestation.Agencies,
	DealID int, //处理序号,用于多次Shopping接驳时处理
	forecycle int, //前面处理到的cycle编号,因为是多次调用处理的
	fore []cacheflight.FlightJSON, //动态航班信息
	mbris []*MB2RouteInfo, //静态票单信息
	segment int, //Journey Segment Code
	DeviationRate int, //绕航率
	ConnMinutes int, /*允许最大接驳时间(总和)*/
	ShowBCC bool) ( /*是否显示无舱位BCC*/
	cycle int) {

	if len(mbris) == 0 || fore == nil ||
		len(fore) < segment || len(fore[segment-1].Route) == 0 {
		return forecycle
	}

	cycle = forecycle
	aur := make(map[string]string, 50) //string1=Routine,string2=agency,加上代理是位了产生重复被使用,因为现在Fare分散了,必须先重复然后最后去重(去最小价格)

	lrout := len(fore[segment-1].Route)

	cache_cis := make(map[uintptr][][]*cacheflight.CabinInfoStruct, lrout) //用于保存适合的航班,最后减少舱位输出([Leg][Code])
	sc := af.ShowShoppingName + "_" + strconv.Itoa(DealID) + "_" + strconv.Itoa(segment) + "_"

	for _, ri := range mbris {
		//远程航班匹配,过滤静态匹配过的票价; ri.doneID如果可以与远程返回的信息判断是否重复就更完美
		if ri.Segment != segment || ri.noget || len(ri.MM) > 0 || len(ri.MMBCC) > 0 {
			continue
		}

		//以下直接循环处理是因为2点:(1)内存缓存换程4维,航线的缓存成为无序;(2)外部获取的航班是无序的.
		for _, fore_rout := range fore[segment-1].Route { //这里是支持单/双程分开匹配航班的.

			if fore_rout.R == ri.Routine &&
				//(ri.Agency == "FB" ||这些条件都被过滤了,只有可能不同的PCC
				//((fore_rout.FI[0].Legs[0].P == ri.Agency ||
				//	fore_rout.FI[0].Legs[0].P == af.DataSource[0]) &&
				fore_rout.FI[0].Legs[0].PCC == ri.PCC {

				if agency, ok := aur[fore_rout.R+fore_rout.RFN+string(ri.Trip)+ri.Agency+ri.PCC]; ok &&
					agency != fore_rout.FI[0].Legs[0].P { //加上Trip是为了分开单/双程.
					continue //过滤被更高优先级处理的航班(Cache需求)
				}
				cycle++
				cis := cache_cis[uintptr(unsafe.Pointer(fore_rout))]

				if recis, add := MainBill2Legs(ri, fore_rout, sc+strconv.Itoa(cycle), cis); add {
					aur[fore_rout.R+fore_rout.RFN+string(ri.Trip)+ri.Agency+ri.PCC] = fore_rout.FI[0].Legs[0].P //记录已处理航班
					cache_cis[uintptr(unsafe.Pointer(fore_rout))] = recis
					cis = recis
				}

				//这里代表是录入的票单啊
				if af.Agency == "FB" {
					//BCC有航班无舱位Shopping,这里转变为制作每各航司最低价//这里无论ShowBCC都要处理.
					if recis, add := MainBill2LegsBCC(ri, fore_rout, sc+strconv.Itoa(cycle), cis, ShowBCC); add {
						aur[fore_rout.R+fore_rout.RFN+string(ri.Trip)+ri.Agency+ri.PCC] = fore_rout.FI[0].Legs[0].P //记录已处理航班
						cache_cis[uintptr(unsafe.Pointer(fore_rout))] = recis
					}
				}
			}
		}
	}

	for _, fore_rout := range fore[segment-1].Route {
		if cis, ok := cache_cis[uintptr(unsafe.Pointer(fore_rout))]; ok {
			for i := 0; i < len(fore_rout.FI[0].Legs); i++ {
				fore_rout.FI[0].Legs[i].CI = cis[i] //这里主要是为了减少输出,把无关的舱位去除.
			}
		}
	}

	return
}

//多段Segment的价格相加计算(其实多段相加必须存在Routine取折扣代码表查询的,不过这里应该不再被使用)
func MatchingJourneyAddprice(
	jti *JourneyTicketInfo, //出票节点,通常指出一张票,但会提供不同供应商&注册公司的资料
	fore *JourneyInfo, //上一Journey信息
	journey *JourneyMiddleInfo, //下一接驳信息,Journey中间信息
	rout *mysqlop.Routine) {

	var (
		//BasePrice    int
		lowprice     int
		SourcePrice  int
		price        int
		AgencyFee    float64
		EncourageFee float64
		TicketFee    int
		AgencyRay    string

		Airline       string
		DepartStation string
		ArriveStation string
		GoorBack      int
		GDS           string
		PCC           string
		Berth         string
	)

	for _, tai := range jti.TicketAgency {
		//单程不再在这里添加价格了,单程作为单独的Ticket处理

		tpi := tai.TicketPricing[0]
		//BasePrice = tpi.BasePrice
		SourcePrice = tpi.SourceBasePrice

		if SourcePrice > journey.Mb.AdultsPrice {
			price = journey.Mb.AdultsPrice
			Airline = journey.Mb.AirInc
			DepartStation = journey.Mb.Springboard
			ArriveStation = journey.Mb.Destination
			GDS = journey.Mb.Agency
			PCC = journey.Mb.PCC
			Berth = journey.Mb.Berth
			GoorBack = 1
		} else {
			price = SourcePrice
			Airline = fore.Mid_journey.Mb.AirInc
			DepartStation = fore.Mid_journey.Mb.Springboard
			ArriveStation = fore.Mid_journey.Mb.Destination
			GDS = fore.Mid_journey.Mb.Agency
			PCC = fore.Mid_journey.Mb.PCC
			Berth = fore.Mid_journey.Mb.Berth
			GoorBack = 0
		}

		lowprice, AgencyFee, EncourageFee, TicketFee, AgencyRay, _ = F_DCPriceCompute(
			tai.OfficeId, Airline, DepartStation, ArriveStation,
			rout.TravelDate, 1, GoorBack, rout.CrossSeason, "INTLAIR", price, Berth, GDS, PCC)

		price = (SourcePrice + journey.Mb.AdultsPrice) / (journey.Mb.Trip + 1) //兼容2个同航司单程组合的双程票单,此时指出一张票.
		lowprice = price - int(float64(price)*(AgencyFee+EncourageFee)/100) + TicketFee

		tpi.BasePrice = lowprice
		tpi.TotalPrice = lowprice
		tpi.SourceBasePrice = price
		tpi.SourceTotalPrice = price
		tpi.AgencyFee = strconv.FormatFloat(AgencyFee, 'f', 2, 64)
		tpi.EncourageFee = strconv.FormatFloat(EncourageFee, 'f', 2, 64)
		tpi.TicketFee = strconv.Itoa(TicketFee)

		tai.AgencyRay = AgencyRay

		tpi = tai.TicketPricing[1]
		//BasePrice = tpi.BasePrice
		SourcePrice = tpi.SourceBasePrice

		price = (SourcePrice + journey.Mb.ChildrenPrice) / (journey.Mb.Trip + 1) //兼容2个同航司单程组合的双程票单,此时指出一张票.
		lowprice = price - int(float64(price)*(AgencyFee+EncourageFee)/100) + TicketFee

		tpi.BasePrice = lowprice
		tpi.TotalPrice = lowprice
		tpi.SourceBasePrice = price
		tpi.SourceTotalPrice = price
		tpi.AgencyFee = strconv.FormatFloat(AgencyFee, 'f', 2, 64)
		tpi.EncourageFee = strconv.FormatFloat(EncourageFee, 'f', 2, 64)
		tpi.TicketFee = strconv.Itoa(TicketFee)

	}
}

//把Segment段的信息及价格相加(被MatchingJourneyIntelligent调用)
func MatchingJourneyAddinfo(
	map_bcc map[string]string, //BCC
	officeids []string,
	fore *JourneyInfo, //已经处理的"前段"航线
	journey *JourneyMiddleInfo, //下一接驳信息
	lenjourney int, //航班长度,用于拷贝fore
	doi int, /*预订的舱位顺序*/
	rout *mysqlop.Routine) *JourneyInfo {

	qjsi := make([]*JourneySegmentInfo, 0, lenjourney)
	qjti := make([]*JourneyTicketInfo, 0, 2) //修改为多出票单号 20160511
	ticketlen := len(fore.JourneyTicket)
	Tax := fore.Currency //这里保存的是临时的Routine,用于税务计算,不是输出的Routine.

	//fore与journey在这函数里面其实匹配的是一一对应关系,只是fore由多航段组成
	for _, js := range fore.JourneySegment { //如果出票前续Journey而1,这里可以简化处理,只做一次而已.
		qjsi = append(qjsi, js.Copy())
	}

	for _, jt := range fore.JourneyTicket { //如果出票前续Journey而1,这里可以简化处理,只做一次而已.
		qjti = append(qjti, jt.Copy())
	}

	//这里加入其他出票的可能,出票与Segment数量不是正比的
	if journey.Mb.Trip == 0 && fore.Mid_journey.Mb.AirInc != journey.Mb.AirInc { //下一段是单程的(前后续双单程必须出一张票)
		ticketlen++

		qjti = append(qjti, createTicketInfo(map_bcc, officeids, journey, rout, strconv.Itoa(ticketlen))...)

		if strings.HasPrefix(journey.Mb.BillID, prefix_DS) { //电商可以作为前后续
			Tax += ";" + prefix_DS + journey.Mb.ConditionID
		} else {
			Tax += ";" + journey.Routine
		}
	} else { //主票单程的会在AddTax计算总的价格,双程(前后续单程)的累积在单个TicketID然后获取

		//更特殊的,可以把Journey使用的[]MiddleJourney登记起来
		//if journey.Mb.Trip == 0 || fore.Mid_journey.Mb.AdultsPrice != journey.Mb.AdultsPrice {
		MatchingJourneyAddprice(qjti[ticketlen-1 /*0*/], fore, journey, rout) //20160512 修改为出多张票
		//}双程是第2段才加价的,MatchingJourneyFirst只对单程加价.

		if journey.afAgency.Agency == "FB" { //!strings.HasPrefix(journey.Mb.BillID, prefix_DS) {
			if Tax[len(Tax)-3:] == journey.Routine[:3] { //税费计算
				Tax += journey.Routine[3:]
			} else {
				Tax += "/" + journey.Routine
			}
		}
	}

	journey.CMs += fore.Mid_journey.CMs //累加中转时间

	return &JourneyInfo{
		Customer: fore.Customer,
		Status:   fore.Status + journey.Status,
		Currency: Tax, //税务由';'分割不同获取
		JourneySegment: append(qjsi, &JourneySegmentInfo{
			SC:     journey.SegmentCode,
			FC:     journey.FareCode,
			TID:    strconv.Itoa(ticketlen),
			NO:     strconv.Itoa(doi + 1),
			Cabins: journey.CI}),
		JourneyTicket: qjti,
		Mid_journey:   journey,
		Routine:       fore.Mid_journey.Routine + journey.Routine[3:],
		FlightNumber:  fore.Mid_journey.FlightNumber + journey.FlightNumber}
}

//这各函数是在MatchingJourneyFirst建立多各用户价格
//同时被MatchingJourneyAddinfo调用(两个单程相加)
func createTicketInfo(
	map_bcc map[string]string, //BCC,这里是bcc最终使用点.
	officeids []string,
	journey *JourneyMiddleInfo,
	rout *mysqlop.Routine,
	TicketId string) []*JourneyTicketInfo {

	qtai := make([]*TicketAgencyInfo, 0, len(officeids))

	for _, officeid := range officeids {

		if len(officeids) > 1 {
			model_2s := cachestation.Model_3[officeid]
			ok := false
			for _, m2 := range model_2s {
				if m2.DataSource == journey.Mb.Agency &&
					(m2.PCC == journey.Mb.PCC ||
						m2.PCC == "" && m2.DataSource != "1E") {
					ok = true
				}
			}
			if !ok {
				continue //这里太繁琐了
			}
		}

		if journey.Mb.BigCustomerCode != "" {
			if map_bcc[officeid+"#"+journey.Mb.AirInc] != journey.Mb.BigCustomerCode {
				continue //注册用户被航司给与不同的BCC
			}
		}

		qtpi := make([]*TicketPriceInfo, 0, 2)

		//可以清晰后面的业务流程
		var (
			lowprice     int
			price        int
			AgencyFee    float64
			EncourageFee float64
			TicketFee    int
			AgencyRay    string
		)

		TicketPrice := func(PassengerType string) {

			if PassengerType == "ADT" {
				lowprice = journey.Mb.AdultsPrice
				price = journey.Mb.AdultsPrice
			} else {
				lowprice = journey.Mb.ChildrenPrice
				price = journey.Mb.ChildrenPrice
			}

			//双程的折扣在MatchingJourneyAddprice处理,因为折扣按小的折扣方式折扣
			if journey.Mb.Trip == 0 {
				if PassengerType == "ADT" {
					lowprice, AgencyFee, EncourageFee, TicketFee, AgencyRay, _ = F_DCPriceCompute(
						officeid, journey.Mb.AirInc, journey.Mb.Springboard, journey.Mb.Destination,
						rout.TravelDate, 0, 0 /*rout.Trip, rout.GoorBack*/, rout.CrossSeason, "INTLAIR", price,
						journey.Mb.Berth, journey.Mb.Agency, journey.Mb.PCC)
				} else {
					if journey.Mb.AdultsPrice != journey.Mb.ChildrenPrice {
						lowprice = price - int(float64(price)*(AgencyFee+EncourageFee)/100) + TicketFee
					} else {
						lowprice = qtpi[0].BasePrice
					}
				}
			}

			qtpi = append(qtpi, &TicketPriceInfo{
				PassengerType:    PassengerType,
				Currency:         "CNY",
				BasePrice:        lowprice,
				TotalPrice:       lowprice,
				SourceBasePrice:  price,
				SourceTotalPrice: price,
				AgencyFee:        strconv.FormatFloat(AgencyFee, 'f', 2, 64),
				EncourageFee:     strconv.FormatFloat(EncourageFee, 'f', 2, 64),
				TicketFee:        strconv.Itoa(TicketFee)})
		}

		TicketPrice("ADT")
		TicketPrice("CHD")

		qtai = append(qtai, &TicketAgencyInfo{
			Agency:        journey.Mb.Agency, //"FB"
			AgencyRay:     AgencyRay,
			OfficeId:      officeid,
			SessionID:     journey.Mb.SessionID,
			TicketPricing: qtpi})
	}

	return []*JourneyTicketInfo{{ //有时候可能因为过滤的问题会返回一个空TicketAgency,但是是应该不空的.(应该是判断recis有问题导致的)
		TicketId:         TicketId,
		MarketingAirline: journey.Mb.AirInc,
		TicketAgency:     qtai}}
}

//JourneyInfo的生成算法(起步)
func MatchingJourneyFirst(
	map_bcc map[string]string, //BCC
	mid_journey []JMIList, //Journey中间信息
	officeids []string, //注册公司
	rout *mysqlop.Routine) ( //查询航班路线
	journeyback []*JourneyInfo) {

	journeyback = make([]*JourneyInfo, 0, len(mid_journey[0]))
	sort.Sort(mid_journey[0])

	for _, journey := range mid_journey[0] {

		ti := createTicketInfo(map_bcc, officeids, journey, rout, "1")
		if len(ti) > 0 && len(ti[0].TicketAgency) > 0 {
			tax := journey.Routine
			if strings.HasPrefix(journey.Mb.BillID, prefix_DS) {
				tax = prefix_DS + journey.Mb.ConditionID
			}
			journeyback = append(journeyback, &JourneyInfo{
				Customer: journey.Mb.BigCustomerCode, //这里获取BCC,因为双程BCC相同.
				Status:   journey.Status,
				Currency: tax, //这里是为了获取税费

				//JourneySegmentInfo的大小与Segment有关
				JourneySegment: []*JourneySegmentInfo{{
					SC:     journey.SegmentCode,
					FC:     journey.FareCode,
					TID:    "1",
					NO:     "1",
					Cabins: journey.CI}},

				//JourneyTicketinfo的大小与出票数量有关,这一不必须过滤注册用户是否适用对应的BCC.
				JourneyTicket: ti, //createTicketInfo(map_bcc, officeids, journey, rout, "1"),

				Mid_journey:  journey,
				Routine:      journey.Routine,
				FlightNumber: journey.FlightNumber})
		}
	}

	return
}

//JourneyInfo的生成算法(复杂匹配)
func MatchingJourneyIntelligent(
	map_bcc map[string]string, //BCC
	officeids []string,
	journeyfore []*JourneyInfo, //已完成的"前段"航班匹配
	mid_journey []JMIList, //Journey中间信息[Segment][]
	doi int, //for mid_journey[doi]
	rout *mysqlop.Routine,
	thePrefixSuffix bool) ( //加入前后续票单的要求(前后续票单Airline相同)
	journeyback []*JourneyInfo) {

	mid_zone := len(journeyfore) * len(mid_journey[doi])
	if mid_zone > 20000 {
		mid_zone = mid_zone / 10
	} else if mid_zone > 10000 {
		mid_zone = mid_zone / 8
	} else if mid_zone > 3000 {
		mid_zone = mid_zone / 6
	} else if mid_zone > 500 {
		mid_zone = mid_zone / 4
	}

	journeyback = make([]*JourneyInfo, 0, mid_zone)
	sort.Sort(mid_journey[doi])
	record := make(map[string]int, 200)

	for _, fore := range journeyfore {
		for _, journey := range mid_journey[doi] {

			if fore.Mid_journey.Mb.Trip != journey.Mb.Trip ||
				//fore.Mid_journey.Mb.Agency != journey.Mb.Agency ||
				fore.Mid_journey.Mb.PCC != journey.Mb.PCC ||
				//fore.Mid_journey.afAgency != journey.afAgency ||

				fore.Mid_journey.afAgency.Agency == "FB" && //其它的匹配是按票单号去匹配的
					((fore.Mid_journey.Mb.Trip == 1 || thePrefixSuffix) && //双程/前后续,必须相同Routine
						fore.Mid_journey.Mb.Routine != RedoRoutineMutil(journey.Mb.Routine) || //这里journey.Routine比journey.Mb.Routine严格一点
						//承接上面逻辑
						fore.Mid_journey.Mb.Trip == 0 && !thePrefixSuffix && //双程2个直飞不同航司,但路程要相同
							(fore.Mid_journey.Mb.Routine == RedoRoutineMutil(journey.Mb.Routine) ||
								ShortRoutineMutil(fore.Mid_journey.Mb.Routine) != ShortRoutineMutil(RedoRoutineMutil(journey.Mb.Routine)))) {
				continue
			}

			if fore.Mid_journey.Mb.Trip == 1 {
				if fore.Mid_journey.Mb.MixBerth != journey.Mb.MixBerth {
					continue
				}

				//不支持混舱
				if fore.Mid_journey.Mb.MixBerth == 3 && //|| journey.Mb.MixBerth == 3) &&
					(fore.Mid_journey.Mb.BillID != journey.Mb.BillID ||
						fore.Mid_journey.Mb.Berth != errorlog.ReverseBerth(journey.Mb.Berth)) {
					continue
				}

				//本票间混舱
				if fore.Mid_journey.Mb.MixBerth == 1 && //|| journey.Mb.MixBerth == 1) &&
					(fore.Mid_journey.Mb.BillID != journey.Mb.BillID || //这个对比也同时解决电商等其它供应商编号相同可以Shopping

						strings.HasPrefix(fore.Mid_journey.Mb.BillID, prefix_FareV2) && //FareV2的混舱在舱位不同的情况下必须Remark相同
							(fore.Mid_journey.Mb.Berth != errorlog.ReverseBerth(journey.Mb.Berth) &&
								fore.Mid_journey.Mb.Remark != journey.Mb.Remark)) {

					continue
				}
			}

			if (len(fore.Mid_journey.Mb.BigCustomerCode) > 0 ||
				len(journey.Mb.BigCustomerCode) > 0) && //都是BCC
				(fore.Mid_journey.Mb.BillID != journey.Mb.BillID ||
					fore.Mid_journey.Mb.BigCustomerCode != journey.Mb.BigCustomerCode) {
				continue
			}

			if len(fore.Mid_journey.Mb.CommendModel) > 0 &&
				len(journey.Mb.CommendModel) > 0 &&
				fore.Mid_journey.Mb.BillID != journey.Mb.BillID {
				continue
			}

			//这里可以控制出混舱等数据的质量,比如相同的Routine只出一条记录.
			//获取Rontine最低的价格,该价格可能时混舱,但小飞飞必须单独一条最低价.
			var ok1, ok2, ok3 bool
			var r1, r2, r3 string
			var bccstatus int
			var price int
			price2 := fore.Mid_journey.Mb.AdultsPrice + journey.Mb.AdultsPrice

			r1 = fore.Mid_journey.Routine +
				journey.Routine + //KeyCache数据双层可能飞不同路线
				strconv.Itoa(fore.Mid_journey.Mb.Trip) + //存在2单组双的情况
				fore.Mid_journey.FlightNumber + journey.FlightNumber //r1必须加上航班,因为是按航班处理的
			//没有没有区分GDS,PCC

			price, ok1 = record[r1] //最低价格,无论是否混舱.
			if ok1 && price >= price2 {
				ok1 = false
			}

			if len(fore.Mid_journey.Mb.CommendModel) > 0 && len(journey.Mb.CommendModel) > 0 {
				r2 = r1 + fore.Mid_journey.Mb.BillID
				price, ok2 = record[r2] //小飞飞最低价格
			}
			if ok2 && price >= price2 {
				ok2 = false
			}

			if fore.Mid_journey.Mb.BigCustomerCode != "" {
				r3 = r1 + fore.Mid_journey.Mb.BillID + fore.Mid_journey.Mb.BigCustomerCode
				price, ok3 = record[r3] //BCC最低价格
				bccstatus = fore.Mid_journey.Status + journey.Status
			}
			if ok3 && price >= price2 {
				ok3 = false
			}

			if true || !ok1 || (r2 != "" && !ok2) || (r3 != "" && !ok3) {
				if !ok1 && bccstatus == 0 {
					record[r1] = price2
				}

				if r2 != "" && !ok2 && bccstatus == 0 {
					record[r2] = price2
				}

				if r3 != "" && !ok3 {
					record[r3] = price2
				}

				journeyback = append(journeyback, MatchingJourneyAddinfo(map_bcc, officeids, fore, journey, len(mid_journey), doi, rout))
			}
		}
	}

	return
}

func MakingJourney(
	mid_journey []JMIList, //Journey中间信息
	p2pi *Point2Point_In, //officeids []string, //注册公司
	rout []*mysqlop.Routine, //查询航班路线
	thePrefixSuffix bool) ( //加入前后续票单的要求(前后续票单Airline相同)
	output []*JourneyInfo) {

	lenrout := len(mid_journey)

	if lenrout == 0 {
		return
	}

	//获取注册用户使用到的BCC
	map_bcc := make(map[string]string, 50) //string = OfficeID + # + Airline
	for _, OfficeID := range p2pi.Offices {
		model_2s, ok := cachestation.Model_3[OfficeID]

		if ok {
			for _, m2 := range model_2s {
				cachestation.DC_Airline_BCC.Mutex.RLock()
				airline_bcc, ok2 := cachestation.DC_Airline_BCC.Airline_BCC[m2.DiscountCode]
				cachestation.DC_Airline_BCC.Mutex.RUnlock()

				if ok2 {
					for airline, bcc := range airline_bcc {
						map_bcc[OfficeID+"#"+airline] = bcc
					}
				}
			}
		}
	}

	output = MatchingJourneyFirst(map_bcc, mid_journey, p2pi.Offices, rout[0])

	for i := 1; i < lenrout; i++ {
		output = MatchingJourneyIntelligent(map_bcc, p2pi.Offices, output, mid_journey, i, rout[i], thePrefixSuffix)
	}

	return output
}

//加税并获取最低价格(Flight与Fare匹配时第1次获取最低价)（---------）
func JourneyAddTax(
	journeyfore []*JourneyInfo,
	thePrefixSuffix bool) { /*前续税的处理方式,这个参数是处理过的*/

	for _, journey := range journeyfore {
		//ti, ok := cachestation.Tax[journey.Currency]
		tp := make([]*TicketPriceInfo, 0, 2) //支持多张出票,tp = TicketPrice

		//如果是FB 或者是 DianShang 的话
		if journey.Mid_journey.afAgency.Agency == "FB" ||
			journey.Mid_journey.afAgency.Agency == "DianShang" {
			for ticketid, tax := range strings.Split(journey.Currency, ";") {

				//thePrefixSuffix == !theTrip && thePrefixSuffix
				if thePrefixSuffix && journey.Mid_journey.Mb.Trip == 0 &&
					!strings.HasPrefix(tax, prefix_DS) {
					tax = tax[:10] //(同航司)前续计税方式:单程*2
				}

				ADTTax, CHDTax := 0, 0
				Agency, LastModDateTime := "", ""

				if strings.HasPrefix(tax, prefix_DS) { //前后续需要
					Agency = journey.Mid_journey.Mb.Agency
					LastModDateTime = ""
					total := strings.Split(tax[len(prefix_DS):], " ")
					if len(total) == 2 {
						ADTTax, _ = strconv.Atoi(total[0])
						CHDTax, _ = strconv.Atoi(total[1])
					} else if len(total) == 1 {
						ADTTax, _ = strconv.Atoi(total[0])
						CHDTax = ADTTax
					}
				} else {
					//cachestation.TaxLock.RLock()
					ti, ok := cachestation.Tax[tax]
					//cachestation.TaxLock.RUnlock()

					if ok {
						Agency = ti.Agency
						LastModDateTime = ti.LastModDateTime

						if thePrefixSuffix && journey.Mid_journey.Mb.Trip == 0 {
							ADTTax = ti.ADTTax * 2
							CHDTax = ti.CHDTax * 2
						} else {
							ADTTax = ti.ADTTax
							CHDTax = ti.CHDTax
						}
					}
				}

				j := journey.JourneyTicket[ticketid].TicketAgency[0].TicketPricing[0] //多出票时,这里可能使用不同的Agency

				//每个供应商的价格都加税
				for i := range journey.JourneyTicket[ticketid].TicketAgency {

					tpi := journey.JourneyTicket[ticketid].TicketAgency[i].TicketPricing

					tpi[0].Tax = strconv.Itoa(ADTTax)
					tpi[0].TaxAgency = Agency //多出票时,这里不是严谨的.
					tpi[0].LastTaxDate = LastModDateTime
					tpi[0].TotalPrice += ADTTax
					tpi[0].SourceTotalPrice += ADTTax

					tpi[1].Tax = strconv.Itoa(CHDTax)
					tpi[1].TaxAgency = Agency
					tpi[1].LastTaxDate = LastModDateTime
					tpi[1].TotalPrice += CHDTax
					tpi[1].SourceTotalPrice += CHDTax

					if j.SourceTotalPrice > tpi[0].SourceTotalPrice {
						j = tpi[0]
					}
				}
				tp = append(tp, j)
			}
		} else {
			ADTTax, CHDTax := 0, 0
			Agency, LastModDateTime := journey.Mid_journey.afAgency.ShowShoppingName, errorlog.DateTime()
			//if si := strings.LastIndex(journey.Mid_journey.Mb.BillID, "-"); si > 14 {
			//	LastModDateTime = journey.Mid_journey.Mb.BillID[si-14 : si]
			//}
			tax := strings.Split(journey.Mid_journey.Mb.ConditionID, " ")
			if len(tax) > 0 {
				ADTTax, _ = strconv.Atoi(tax[0])
			}

			if len(tax) == 2 {
				CHDTax, _ = strconv.Atoi(tax[1])
			} else {
				CHDTax = ADTTax
			}

			j := journey.JourneyTicket[0].TicketAgency[0].TicketPricing[0] //多出票时,这里可能使用不同的Agency

			//每个供应商的价格都加税
			for i := range journey.JourneyTicket[0].TicketAgency {

				tpi := journey.JourneyTicket[0].TicketAgency[i].TicketPricing

				tpi[0].Tax = strconv.Itoa(ADTTax)
				tpi[0].TaxAgency = Agency //多出票时,这里不是严谨的.
				tpi[0].LastTaxDate = LastModDateTime
				tpi[0].TotalPrice += ADTTax
				tpi[0].SourceTotalPrice += ADTTax

				tpi[1].Tax = strconv.Itoa(CHDTax)
				tpi[1].TaxAgency = Agency
				tpi[1].LastTaxDate = LastModDateTime
				tpi[1].TotalPrice += CHDTax
				tpi[1].SourceTotalPrice += CHDTax

				if j.SourceTotalPrice > tpi[0].SourceTotalPrice {
					j = tpi[0]
				}
			}
			tp = append(tp, j)
		}

		//按出票数量计算总价格
		for k, v := range tp {
			if k == 0 {
				journey.Currency = v.Currency
				journey.Tax = v.Tax
				journey.BasePrice = v.BasePrice
				journey.TotalPrice = v.TotalPrice
				journey.SourceBasePrice = v.SourceBasePrice
				journey.SourceTotalPrice = v.SourceTotalPrice
			} else {
				tax1, tax2 := 0, 0
				if journey.Tax != "" {
					tax1, _ = strconv.Atoi(journey.Tax)
				}
				if v.Tax != "" {
					tax2, _ = strconv.Atoi(v.Tax)
				}

				journey.Tax = strconv.Itoa(tax1 + tax2)
				journey.BasePrice += v.BasePrice
				journey.TotalPrice += v.TotalPrice
				journey.SourceBasePrice += v.SourceBasePrice
				journey.SourceTotalPrice += v.SourceTotalPrice
			}
		}
	}
}

//Mark No Get
func markNoget(mbris []*MB2RouteInfo) {
	for _, ri := range mbris {
		if len(ri.MM) == 0 && len(ri.MMBCC) == 0 && !ri.noget && //目前不对非FB远程操作,因为远程接口未开发.
			(ri.MatchCount > 0 || ri.af.Agency != "FB" || !CheckOagRoutine(ri.Routine[:10], ri.TravelDate)) {
			ri.noget = true
		}
	}
}

//Point2Point_The2需要制作JourneyMiddleInfo的航线补全(这里假设Segment数量为2)
func InterJourney(
	mbris []*MB2RouteInfo, /*待处理的RI*/
	Trip int, /*航程 1单程 2双程*/
	second bool, //最后制作Journey的参数,要求来回程完全匹配有数据
	thePrefixSuffix bool) ( //加入前后续票单的要求(前后续票单Airline相同)
	done []*MB2RouteInfo, /*已经完成航班匹配的RI*/
	back []*MB2RouteInfo /*未完成航班匹配的RI*/) {

	done = make([]*MB2RouteInfo, 0, len(mbris))
	back = make([]*MB2RouteInfo, 0, len(mbris))

	for _, ri := range mbris {
		ri.matched = false
		ri.diffRoutine = false
		ri.sameRoutine = false

		if len(ri.MM) == 0 && len(ri.MMBCC) == 0 && !ri.noget && ri.MatchCount > 0 {
			ri.noget = true
		}
	}

	if Trip == 1 {
		for _, ri := range mbris {
			if len(ri.MM) > 0 || len(ri.MMBCC) > 0 {
				done = append(done, ri)
			} else {
				if !ri.noget {
					back = append(back, ri)
				}
			}
		}

	} else if Trip == 2 {

		billids := make(map[string]int, 200) //保存电商数据,2条相同就可以不被过滤
		for _, ri := range mbris {
			for _, mb := range ri.ListMB {
				if strings.HasPrefix(mb.BillID, prefix_DS) {
					billids[mb.BillID]++
				}
			}
		}

		for _, ri1 := range mbris {
			if ri1.Segment == 2 {
				continue
			}

			for _, ri2 := range mbris {
				if ri2.Segment == 1 {
					continue
				}

				if ri1.Trip == ri2.Trip &&
					ri1.Agency == ri2.Agency &&
					ri1.PCC == ri2.PCC &&
					(!second || (len(ri1.MM) > 0 || len(ri1.MMBCC) > 0) &&
						(len(ri2.MM) > 0 || len(ri2.MMBCC) > 0)) { //单双程混合时分组

					//其实就是出了主票单,前续由2个单程组成,当时开发的时候做多了功能
					if (ri1.Trip == 1 || thePrefixSuffix) && //双程数据 or 单程要求前后续相同航司
						ri1.Routine == RedoRoutineMutil(ri2.Routine) || //Routine包含Airline
						(ri1.Trip == 0 && !thePrefixSuffix) && //单程数据, 要求前后续不同航司
							ri1.Routine != RedoRoutineMutil(ri2.Routine) &&
							ShortRoutineMutil(ri1.Routine) == ShortRoutineMutil(RedoRoutineMutil(ri2.Routine)) {

						if !ri1.noget && !ri2.noget {
							if !ri1.matched {
								if len(ri1.MM) > 0 || len(ri1.MMBCC) > 0 {
									done = append(done, ri1)
								} else {
									back = append(back, ri1)
								}
								ri1.matched = true //防止1对多时重复加载
							}

							if !ri2.matched {
								if len(ri2.MM) > 0 || len(ri2.MMBCC) > 0 {
									done = append(done, ri2)
								} else {
									back = append(back, ri2)
								}
								ri2.matched = true
							}

							if ri1.Routine == RedoRoutineMutil(ri2.Routine) {
								ri1.sameRoutine = true
								ri2.sameRoutine = true
							} else {
								ri1.diffRoutine = true
								ri2.diffRoutine = true
							}
						}
						//break这里不可以break.因为单程中,1个单对应其他多个单
					}
				}
			}
		}

		for _, ri := range mbris {
			if /*ri.prefixDS*/ ri.af.Agency != "FB" && !ri.noget && !ri.matched {
				mbl := len(ri.ListMB)
				for i := 0; i < mbl; {
					if billids[ri.ListMB[i].BillID] == 2 {
						ri.sameRoutine = true //电商数据没有过滤的舱位
						i++
					} else {
						mbl--
						ri.ListMB[i], ri.ListMB[mbl] = ri.ListMB[mbl], ri.ListMB[i]
					}
				}

				if mbl > 0 {
					if mbl != len(ri.ListMB) {
						ri.ListMB = ri.ListMB[:mbl]
						//sort.Sort(ri.ListMB)已经不再是按最低价格处理
					}
					if len(ri.MM) > 0 || len(ri.MMBCC) > 0 {
						done = append(done, ri)
					} else {
						back = append(back, ri)
					}
				}

			}
		}

		if !second {
			for _, ri := range back {
				if ri.diffRoutine && !ri.sameRoutine { //把混舱的删除
					L := len(ri.ListMB)
					for i := 0; i < L; {
						if ri.ListMB[i].MixBerth == 3 || ri.ListMB[i].MixBerth == 1 {
							L--
							ri.ListMB[i], ri.ListMB[L] = ri.ListMB[L], ri.ListMB[i]
						} else {
							i++
						}
					}

					if L != len(ri.ListMB) {
						ri.ListMB = ri.ListMB[:L]
						//sort.Sort(ri.ListMB)已经不再是按最低价格处理
					}
				}
			}

			backLen := len(back)
			for i := 0; i < backLen; {
				if len(back[i].ListMB) == 0 {
					backLen--
					back[i], back[backLen] = back[backLen], back[i]
				} else {
					i++
				}
			}

			if backLen != len(back) {
				back = back[:backLen]
			}
		}

	}
	return
}

//合并MiddleMatch(MM<--MMBCC)
func MergeMiddleMatch(mbris []*MB2RouteInfo, Trip int) {

	routine_price := make(map[string]struct{}, 100)

	if Trip == 1 {
		for _, ri := range mbris {
			if len(ri.MMBCC) == 0 {
				continue
			}

			middleJourneyList := make(JMIList, 0, 20)
			for _, bcc := range ri.MMBCC {
				middleJourneyList = append(middleJourneyList, bcc.output...)
			}
			if len(middleJourneyList) == 0 {
				continue
			}
			sort.Sort(middleJourneyList)

			//排列MMBCC
			for _, output := range middleJourneyList {
				if output.Status == 0 {
					//每一Routine只出一条数据
					if _, ok := routine_price[ri.Routine]; !ok {
						output.MM.output = output.MM.output[:0]
						output.MM.output = append(output.MM.output, output)
						ri.MM = append(ri.MM, output.MM)
						routine_price[ri.Routine] = struct{}{}
					}
				}
			}

			for _, output := range middleJourneyList {
				if output.Status == 1 {
					if _, ok := routine_price[ri.Routine]; !ok {
						output.MM.output = output.MM.output[:0]
						output.MM.output = append(output.MM.output, output)
						ri.MM = append(ri.MM, output.MM)
						routine_price[ri.Routine] = struct{}{}
					}
				}
			}
		}

	} else { //Trip==2
		for _, ri1 := range mbris {
			if ri1.Segment == 2 || len(ri1.MMBCC) == 0 ||
				ri1.Trip == 0 { //BCC没有2票合并,Fare是单双层混一起的.
				continue
			}

			middleJourneyList_1 := make(JMIList, 0, 20)
			for _, bcc := range ri1.MMBCC {
				middleJourneyList_1 = append(middleJourneyList_1, bcc.output...)
			}
			sort.Sort(middleJourneyList_1)

			for _, ri2 := range mbris {
				if ri2.Segment == 1 || len(ri2.MMBCC) == 0 || ri2.Trip == 0 ||
					ri1.Routine != RedoRoutineMutil(ri2.Routine) {
					continue
				}

				middleJourneyList_2 := make(JMIList, 0, 20)
				for _, bcc := range ri2.MMBCC {
					middleJourneyList_2 = append(middleJourneyList_2, bcc.output...)
				}
				sort.Sort(middleJourneyList_2)

				//匹配双层最低有位舱位
				for _, out1 := range middleJourneyList_1 {
					if out1.Status == 1 { //有航班无舱位
						continue
					}

					for _, out2 := range middleJourneyList_2 {
						if out1.Mb.MixBerth != out2.Mb.MixBerth || out2.Status == 1 ||
							out1.Mb.BillID != out2.Mb.BillID ||
							out1.Mb.BigCustomerCode != out2.Mb.BigCustomerCode ||
							(out1.Mb.MixBerth == 3 &&
								out1.Mb.Berth != errorlog.ReverseBerth(out2.Mb.Berth)) {
							continue
						}

						r := out1.Routine + strconv.Itoa(out1.Mb.MixBerth)
						if out1.Mb.MixBerth == 3 {
							r += out1.Mb.Berth
						}
						if _, ok := routine_price[r]; !ok {
							out1.MM.output = out1.MM.output[:0]
							out1.MM.output = append(out1.MM.output, out1)
							ri1.MM = append(ri1.MM, out1.MM)
							out2.MM.output = out2.MM.output[:0]
							out2.MM.output = append(out2.MM.output, out2)
							ri2.MM = append(ri2.MM, out2.MM)
							routine_price[r] = struct{}{}
						}
					}
				}

				//匹配双层最低无位舱位
				for _, out1 := range middleJourneyList_1 {
					for _, out2 := range middleJourneyList_2 {
						if out1.Mb.MixBerth != out2.Mb.MixBerth ||
							out1.Mb.BillID != out2.Mb.BillID ||
							out1.Mb.BigCustomerCode != out2.Mb.BigCustomerCode ||
							(out1.Mb.MixBerth == 3 &&
								out1.Mb.Berth != errorlog.ReverseBerth(out2.Mb.Berth)) {
							continue
						}

						r := out1.Routine + strconv.Itoa(out1.Mb.MixBerth)
						if out1.Mb.MixBerth == 3 {
							r += out1.Mb.Berth
						}
						if _, ok := routine_price[r]; !ok {
							out1.MM.output = out1.MM.output[:0]
							out1.MM.output = append(out1.MM.output, out1)
							ri1.MM = append(ri1.MM, out1.MM)
							out2.MM.output = out2.MM.output[:0]
							out2.MM.output = append(out2.MM.output, out2)
							ri2.MM = append(ri2.MM, out2.MM)
							routine_price[r] = struct{}{}
						}
					}
				}
			}
		}
	}

}

//专门合并成为shopping的处理函数
func ToShopping(
	p2pi *Point2Point_In, //查询条件
	rout []*mysqlop.Routine, //rout是真实取获取的行段
	rmb mysqlop.ResultMainBill, //查询的航班结果集
	theTrip bool, //加入前续票单的要求(主票单Trip=1),主要用于控制票单Trip出票组合
	thePrefixSuffix bool, //加入前后续票单的要求(前后续票单Airline相同),主要用于控制Airline出票组合
	mbris []*MB2RouteInfo) *Point2Point_Output {

	var (
		lenSeg       = len(rout)         //rout是真实取获取的行段
		mid          map[int]struct{}    //Record MainBill ID
		mid_journey  []JMIList           //把各Segment-->Journey的中间结果保存
		p2p_out_json *Point2Point_Output //shopping out
	)

	if rmb.Mainbill == nil {
		for _, ris := range mbris {
			rmb.Mainbill = append(rmb.Mainbill, ris.ListMB...)
		}
	}

	mid = make(map[int]struct{}, len(rmb.Mainbill))
	p2p_out_json = &Point2Point_Output{
		Result:  1,
		Segment: make([]*Point2Point_RoutineInfo2Segment, 0, 200),
		Fare:    make([]*FareInfo, 0, len(rmb.Mainbill))}

	mid_journey = make([]JMIList, lenSeg, lenSeg)
	for i := range rout {
		mid_journey[i] = make([]*JourneyMiddleInfo, 0, 200)
	}

	for _, ri := range mbris { //处理Mid Journey!
		for _, mm := range ri.MM {
			p2p_out_json.Segment = append(p2p_out_json.Segment, mm.fjson)
		}

		for m := range ri.MM { //len(doneID)==len(output)==len(fjson)
			mid_journey[ri.Segment-1] = append(mid_journey[ri.Segment-1], ri.MM[m].output...) //Segment start in 1

			for _, n := range ri.MM[m].doneID { //登记需要使用的MainBill
				mid[n] = struct{}{}
			}
		}
	}

	//排列MiddleJourney价格
	for _, journey := range mid_journey { //没有中途数据直接返回
		if len(journey) == 0 {
			return &Point2Point_Output{
				Result:  100,
				Segment: []*Point2Point_RoutineInfo2Segment{},
				Fare:    []*FareInfo{}}
		}
	}

	for _, rid := range rmb.Mainbill { //添加Shopping.Fare节点
		if _, ok := mid[rid.ID]; ok {
			p2p_out_json.Fare = append(p2p_out_json.Fare,
				&FareInfo{
					FareCode: strconv.Itoa(rid.ID),
					MainBill: rid})
		}
	}

	p2p_out_json.Journey = MakingJourney(mid_journey, p2pi, rout, thePrefixSuffix)
	//fmt.Println("Journey Len", len(p2p_out_json.Journey)) ///////////

	JourneyAddTax(p2p_out_json.Journey, !theTrip && thePrefixSuffix)

	//排列价格及控制输出量
	if len(p2p_out_json.Journey) == 0 && !p2pi.Debug {
		return &Point2Point_Output{ //票单合并时产生出错,所以只能在直接返回时处理
			Result:  100,
			Segment: []*Point2Point_RoutineInfo2Segment{},
			Fare:    []*FareInfo{}}
	} else {
		//这里操作去重Journey,重复是因为MB2RI分配时出现重复,而重复的原因时因为B2 Fare不明确
		minJourney := make(map[string]*JourneyInfo, len(p2p_out_json.Journey))
		for _, JI := range p2p_out_json.Journey {
			if minJI, ok := minJourney[JI.Routine+JI.FlightNumber]; !ok {
				minJourney[JI.Routine+JI.FlightNumber] = JI
			} else {
				if JI.TotalPrice < minJI.TotalPrice {
					minJourney[JI.Routine+JI.FlightNumber] = JI
				}
			}
		}
		p2p_out_json.Journey = p2p_out_json.Journey[:0]
		for _, JI := range minJourney {
			p2p_out_json.Journey = append(p2p_out_json.Journey, JI)
		}

		sort.Sort(p2p_out_json.Journey)
		if p2pi.MaxOutput >= 100 && len(p2p_out_json.Journey) > p2pi.MaxOutput {
			p2p_out_json.Journey = p2p_out_json.Journey[:p2pi.MaxOutput]
		} //暂时屏蔽做大输出数
		MergeSegmentFlight(p2p_out_json)
	}
	return p2p_out_json
}

//接受Point2Point匹配后的结果,然后处理成shopping个是
func AcceptP2P_V1(
	p2pi *Point2Point_In, //查询条件
	rout []*mysqlop.Routine, //rout是真实取获取的行段
	quickout chan []*MB2RouteInfo, //多供应商
	shoppingout chan *Point2Point_Output,
	quickcount int,
	finalout chan *Point2Point_Output) {

	var (
		mbris            []*MB2RouteInfo     //MainBill Routine Info
		p2p_out_json_all *Point2Point_Output //shopping out
	)

	//把快速操作的其他平台供应商合并起来
	if p2pi.Quick {
		for i := 0; i < quickcount; i++ {
			mbris = append(mbris, (<-quickout)...)
			//这里必须处理Timeout免得动态改变AgencyFrame时出现异常
		}
		p2p_out_json_all = ToShopping(p2pi, rout, mysqlop.ResultMainBill{}, false, false, mbris)
	} else {
		for i := 0; i < quickcount; i++ {
			if i == 0 {
				p2p_out_json_all = <-shoppingout
			} else {
				tmp := <-shoppingout
				p2p_out_json_all.Segment = append(p2p_out_json_all.Segment, tmp.Segment...)
				p2p_out_json_all.Fare = append(p2p_out_json_all.Fare, tmp.Fare...)
				p2p_out_json_all.Journey = append(p2p_out_json_all.Journey, tmp.Journey...)
			}
			//这里必须处理Timeout免得动态改变AgencyFrame时出现异常
		}
	}
	finalout <- p2p_out_json_all
}

//专用于2票合并shopping(原始)
//类似汕头去北京，北京去旧金山
func Point2Point_V2(
	QuickShopping bool, //是否快速票单
	dc *DebugControl, //写Debug日志
	rmb mysqlop.ResultMainBill, //查询的航班结果集
	qfi_fore []cacheflight.FlightJSON, //航班动态数据(缓存)[rout=Go/Back]
	DealID int, //处理序号,用于多次Shopping接驳时处理
	p2pi *Point2Point_In, //查询条件
	rout []*mysqlop.Routine, //rout是真实取获取的行段
	theTrip bool, //加入前续票单的要求(主票单Trip=1),主要用于控制票单Trip出票组合
	thePrefixSuffix bool, //加入前后续票单的要求(前后续票单Airline相同),主要用于控制Airline出票组合
	shoppingout chan *Point2Point_Output) {

	if rout == nil {
		//制作
		rout = FlightLegs2Routine(p2pi.Flight)
	}

	var (
		qfi_fore_web []cacheflight.FlightJSON //[rout] 航班动态数据(远程)
		mbris        []*MB2RouteInfo          //MainBill Routine Info
		mid          map[int]struct{}         //Record MainBill ID
		mid_journey  []JMIList                //把各Segment-->Journey的中间结果保存
		p2p_out_json *Point2Point_Output      //shopping out
		cycle        int                      //用于航班编号使用
		lenSeg       = len(rout)              //rout是真实取获取的行段
	)

	if p2pi.Debug && QuickShopping {
		writeLog(dc, DealID, rmb, qfi_fore, nil)
	}

	fLen := len(qfi_fore[0].Route)
	if lenSeg == 2 {
		fLen += len(qfi_fore[1].Route)
	}

	p2p_out_json = &Point2Point_Output{
		Result:  1,
		Segment: make([]*Point2Point_RoutineInfo2Segment, 0, fLen),
		Fare:    make([]*FareInfo, 0, len(rmb.Mainbill))}

	mid = make(map[int]struct{}, len(rmb.Mainbill))

	fLen = fLen * len(rmb.Mainbill)
	if lenSeg == 2 {
		fLen = fLen / 4 //4== fLen/2 * len(rmb.Mainbill)/2 来回程
	}
	if fLen > 300 {
		fLen = 300
	}
	mid_journey = make([]JMIList, lenSeg, lenSeg)
	for i := range rout {
		mid_journey[i] = make([]*JourneyMiddleInfo, 0, fLen)
	}

	_, mbris = InterJourney(MainBill2RouteInfo(cachestation.AgencyFrame[0], rmb, rout), lenSeg, false, thePrefixSuffix) //这里过滤当天没飞及查询不到远程数据的航班

	//过滤无效航班
	escape := make(map[string]struct{}, 30)
	for _, mb := range mbris {
		if mb.Agency == "FB" {
			escape[mb.Routine] = struct{}{}
		} else {
			escape[mb.Routine+"/"+mb.Agency+mb.PCC] = struct{}{}
		}
	}

	for seg, f := range qfi_fore {
		//fmt.Println("seg=", seg, len(f.Route))
		newfore := make([]*cacheflight.RoutineInfoStruct, 0, len(f.Route))
		ok1, ok2 := false, false
		for _, rout := range f.Route {
			_, ok1 = escape[rout.R+"/"+rout.FI[0].Legs[0].P+rout.FI[0].Legs[0].PCC]
			_, ok2 = escape[rout.R]
			if ok1 || ok2 {
				cilen := 0
				for cilen := 0; cilen < len(rout.FI[0].Legs[0].CI); cilen++ {
					if rout.FI[0].Legs[0].CI[cilen].WI == 2 {
						break
					}
				}
				if cilen != len(rout.FI[0].Legs[0].CI) {
					newfore = append(newfore, rout)
				}
			}
		}
		qfi_fore[seg].Route = newfore
		sort.Sort(qfi_fore[seg].Route)
	}

	for i := range rout { //i+1 == rout[i].Segment
		cycle = MainBillMatchingLegs(cachestation.AgencyFrame[0], DealID, cycle, qfi_fore, mbris, rout[i].Segment, p2pi.DeviationRate, p2pi.ConnMinutes, p2pi.ShowBCC)
	}

	if !QuickShopping {
		markNoget(mbris) //标识掉部分不应该取获取的

		type getFlight struct {
			Segment       int
			TravelDate    string
			ListAirline1E []string
			ListAirline1B []string
			c_fjson       chan *cacheflight.FlightJSON
		}
		getData := make(map[string]*getFlight, 10) //string=DepartStation+ArriveStation
		hadget := make(map[string]struct{}, 20)    //已经远程获取数据的航线

		//把已经成功获取的MB.ID记录起来,这里同时必须记录Routine生成的所在'$'分隔才是更合理的.
		for _, ri := range mbris {
			for _, mm := range ri.MM {
				for _, id := range mm.doneID {
					mid[id] = struct{}{}
				}
			}
			for _, mm := range ri.MMBCC {
				for _, id := range mm.doneID {
					mid[id] = struct{}{}
				}
			}
		}

		for _, ri := range mbris {
			if len(ri.MM) > 0 || len(ri.MMBCC) > 0 || ri.noget {
				continue
			}

			//这里处理,如果一条Fare记录已经存在Shopping成功的Routine,那么不再到远程获取没成功的Routine.
			comeback := true
			for _, mb := range ri.ListMB {
				if mb.Agency != "FB" && mb.BillID != "FareV2" { //只对FB数据源做远程获取数据
					continue
				}
				if _, ok := mid[mb.ID]; !ok {
					comeback = false //还可以再提高：如果同航线低舱位有了,不同再获取高舱位航班
					break
				}
			}
			if comeback {
				continue
			}

			DepartStation := ri.Routine[:3]
			ArriveStation := ri.Routine[len(ri.Routine)-3:]
			Airline := ri.ListMB[0].AirInc //这里绝大多数是正确的,如果航线中存在不同航司,也会是同集团的.

			if _, ok := hadget[DepartStation+Airline+ArriveStation]; ok { //这种组合只是因为远程会自动返回多种中转
				continue //还可以再提高：如果同航线低舱位有了,不同再获取高舱位航班
			} else {
				hadget[DepartStation+Airline+ArriveStation] = struct{}{}
			}

			if getflight, ok := getData[DepartStation+ArriveStation]; ok {
				if Airline1E[Airline] {
					getflight.ListAirline1E = append(getflight.ListAirline1E, Airline)
				} else {
					getflight.ListAirline1B = append(getflight.ListAirline1B, Airline)
				}
			} else {
				getflight = &getFlight{
					Segment:       ri.Segment,
					TravelDate:    ri.TravelDate,
					ListAirline1E: []string{},
					ListAirline1B: []string{},
					c_fjson:       make(chan *cacheflight.FlightJSON, 2)}

				if Airline1E[Airline] {
					getflight.ListAirline1E = append(getflight.ListAirline1E, Airline)
				} else {
					getflight.ListAirline1B = append(getflight.ListAirline1B, Airline)
				}

				getData[DepartStation+ArriveStation] = getflight
			}
		}

		if len(getData) == 0 {
			goto No_getData //直接跳过,这样减少步骤,会快很多
		}

		for DepartArrive, Get := range getData {
			if len(Get.ListAirline1E) > 0 {
				go outsideapi.SearchFlightJSON_TO_V2(DepartArrive[:3], DepartArrive[3:], Get.TravelDate, strings.Join(Get.ListAirline1E, " "), "1E", Get.c_fjson)
			} else {
				Get.c_fjson <- nullFlightJSON
			}

			if len(Get.ListAirline1B) > 0 {
				go outsideapi.SearchFlightJSON_TO_V2(DepartArrive[:3], DepartArrive[3:], Get.TravelDate, strings.Join(Get.ListAirline1B, " "), "1B", Get.c_fjson)
			} else {
				Get.c_fjson <- nullFlightJSON
			}
		}

		qfi_fore_web = make([]cacheflight.FlightJSON, lenSeg, lenSeg)
		for i := range qfi_fore_web {
			qfi_fore_web[i] = cacheflight.FlightJSON{Route: make([]*cacheflight.RoutineInfoStruct, 0, 100)}
		}

		for _, Get := range getData {
			outout := <-Get.c_fjson
			if outout.Route != nil {
				if Get.Segment == 1 {
					qfi_fore_web[0].Route = append(qfi_fore_web[0].Route, outout.Route...)
				} else {
					qfi_fore_web[1].Route = append(qfi_fore_web[1].Route, outout.Route...)
				}
			}

			outout = <-Get.c_fjson
			if outout.Route != nil {
				if Get.Segment == 1 {
					qfi_fore_web[0].Route = append(qfi_fore_web[0].Route, outout.Route...)
				} else {
					qfi_fore_web[1].Route = append(qfi_fore_web[1].Route, outout.Route...)
				}
			}
		}

		for i := range rout { //这里只能假设远程航班并未返回其他有航司航班了！(为了检测Routine时就不匹配了)
			cycle = MainBillMatchingLegs(cachestation.AgencyFrame[0], DealID, cycle, qfi_fore_web, mbris, rout[i].Segment, p2pi.DeviationRate, p2pi.ConnMinutes, p2pi.ShowBCC)
		}

	No_getData:
		if p2pi.Debug {
			if len(getData) > 0 {
				writeLog(dc, DealID, rmb, qfi_fore, qfi_fore_web)
			} else {
				writeLog(dc, DealID, rmb, qfi_fore, nil)
			}
		}
	}

	done, _ := InterJourney(mbris, lenSeg, true, thePrefixSuffix) //获取匹配完整的航线

	//这里合并ri.MM && ri.MMBCC,合并是未了较少BCC无效的输出,并排列MidJourney的价格.
	MergeMiddleMatch(done, lenSeg)

	for _, ri := range done { //处理Mid Journey!
		for _, mm := range ri.MM {
			p2p_out_json.Segment = append(p2p_out_json.Segment, mm.fjson)
		}

		for m := range ri.MM { //len(doneID)==len(output)==len(fjson)
			mid_journey[ri.Segment-1] = append(mid_journey[ri.Segment-1], ri.MM[m].output...) //Segment start in 1

			for _, n := range ri.MM[m].doneID { //登记需要使用的MainBill
				mid[n] = struct{}{}
			}
		}
	}

	//排列MiddleJourney价格
	for _, journey := range mid_journey { //没有中途数据直接返回
		if len(journey) == 0 {
			shoppingout <- &Point2Point_Output{
				Result:  100,
				Segment: []*Point2Point_RoutineInfo2Segment{},
				Fare:    []*FareInfo{}}
			return
		}
	}

	for _, rid := range rmb.Mainbill { //添加Shopping.Fare节点
		if _, ok := mid[rid.ID]; ok {
			p2p_out_json.Fare = append(p2p_out_json.Fare,
				&FareInfo{
					FareCode: strconv.Itoa(rid.ID),
					MainBill: rid})
		}
	}

	p2p_out_json.Journey = MakingJourney(mid_journey, p2pi, rout, thePrefixSuffix)
	//fmt.Println("Journey Len", len(p2p_out_json.Journey)) ///////////

	JourneyAddTax(p2p_out_json.Journey, !theTrip && thePrefixSuffix)

	//排列价格及控制输出量
	if len(p2p_out_json.Journey) == 0 && !p2pi.Debug {
		shoppingout <- &Point2Point_Output{ //票单合并时产生出错,所以只能在直接返回时处理
			Result:  100,
			Segment: []*Point2Point_RoutineInfo2Segment{},
			Fare:    []*FareInfo{}}
	} else {
		//这里操作去重Journey,重复是因为MB2RI分配时出现重复,而重复的原因时因为B2 Fare不明确
		minJourney := make(map[string]*JourneyInfo, len(p2p_out_json.Journey))
		for _, JI := range p2p_out_json.Journey {
			if minJI, ok := minJourney[JI.Routine+JI.FlightNumber]; !ok {
				minJourney[JI.Routine+JI.FlightNumber] = JI
			} else {
				if JI.TotalPrice < minJI.TotalPrice {
					minJourney[JI.Routine+JI.FlightNumber] = JI
				}
			}
		}

		p2p_out_json.Journey = p2p_out_json.Journey[:0]
		for _, JI := range minJourney {
			p2p_out_json.Journey = append(p2p_out_json.Journey, JI)
		}

		sort.Sort(p2p_out_json.Journey)
		if p2pi.MaxOutput >= 100 && len(p2p_out_json.Journey) > p2pi.MaxOutput {
			p2p_out_json.Journey = p2p_out_json.Journey[:p2pi.MaxOutput]
		}
		MergeSegmentFlight(p2p_out_json)
		shoppingout <- p2p_out_json
	}
}

//专用于Cache的shopping
//# TODO 这是普通的流程
func Point2Point_V3(
	QuickShopping bool, //是否快速票单
	dc *DebugControl, //写Debug日志
	rmb mysqlop.ResultMainBill, //查询的航班结果集
	qfi_fore []cacheflight.FlightJSON, //航班动态数据(缓存)[rout=Go/Back]
	DealID int, //处理序号,用于多次Shopping接驳时处理
	p2pi *Point2Point_In, //查询条件
	rout []*mysqlop.Routine, //rout是真实取获取的行段
	theTrip bool, //加入前续票单的要求(要求主票单Trip=1),主要用于控制票单Trip出票组合
	thePrefixSuffix bool, //加入前后续票单的要求(前后续票单Airline相同),主要用于控制Airline出票组合
	af *cachestation.Agencies, //供应商资料
	quickout chan []*MB2RouteInfo,
	shoppingout chan *Point2Point_Output) {

	if rout == nil {
		rout = FlightLegs2Routine(p2pi.Flight)
	}

	var (
		qfi_fore_web []cacheflight.FlightJSON //[rout] 航班动态数据(远程)
		mbris        []*MB2RouteInfo          //MainBill Routine Info
		mid          map[int]struct{}         //Record MainBill ID
		cycle        int                      //用于航班编号使用
		lenSeg       = len(rout)              //rout是真实取获取的行段
	)

	if p2pi.Debug && QuickShopping {
		writeLog(dc, DealID, rmb, qfi_fore, nil)
	}

	mid = make(map[int]struct{}, len(rmb.Mainbill))

	_, mbris = InterJourney(MainBill2RouteInfo(af, rmb, rout), lenSeg, false, thePrefixSuffix) //这里过滤当天没飞及查询不到远程数据的航班

	//过滤无效航班
	escape := make(map[string]struct{}, 30)
	for _, mb := range mbris {
		if mb.Agency == "FB" {
			escape[mb.Routine] = struct{}{}
		} else {
			escape[mb.Routine+"/"+mb.Agency+mb.PCC] = struct{}{}
		}
	}

	for seg, f := range qfi_fore {
		//fmt.Println("seg=", seg, len(f.Route))
		newfore := make([]*cacheflight.RoutineInfoStruct, 0, len(f.Route))
		ok1, ok2 := false, false
		for _, rout := range f.Route {
			_, ok1 = escape[rout.R+"/"+rout.FI[0].Legs[0].P+rout.FI[0].Legs[0].PCC]
			_, ok2 = escape[rout.R]
			if ok1 || ok2 {
				cilen := 0
				for cilen := 0; cilen < len(rout.FI[0].Legs[0].CI); cilen++ {
					if rout.FI[0].Legs[0].CI[cilen].WI == 2 {
						break
					}
				}
				if cilen != len(rout.FI[0].Legs[0].CI) {
					newfore = append(newfore, rout)
				}
			}
		}
		qfi_fore[seg].Route = newfore
		sort.Sort(qfi_fore[seg].Route)
	}

	for i := range rout { //i+1 == rout[i].Segment
		cycle = MainBillMatchingLegs(af, DealID, cycle, qfi_fore, mbris, rout[i].Segment, p2pi.DeviationRate, p2pi.ConnMinutes, p2pi.ShowBCC)
	}

	if !QuickShopping && af.Agency == "FB" {
		markNoget(mbris) //标识掉部分不应该取获取的

		type getFlight struct {
			Segment       int
			TravelDate    string
			ListAirline1E []string
			ListAirline1B []string
			c_fjson       chan *cacheflight.FlightJSON
		}
		getData := make(map[string]*getFlight, 10) //string=DepartStation+ArriveStation
		hadget := make(map[string]struct{}, 20)    //已经远程获取数据的航线

		//把已经成功获取的MB.ID记录起来,这里同时必须记录Routine生成的所在'$'分隔才是更合理的.
		for _, ri := range mbris {
			for _, mm := range ri.MM {
				for _, id := range mm.doneID {
					mid[id] = struct{}{}
				}
			}
			for _, mm := range ri.MMBCC {
				for _, id := range mm.doneID {
					mid[id] = struct{}{}
				}
			}
		}

		for _, ri := range mbris {
			if len(ri.MM) > 0 || len(ri.MMBCC) > 0 || ri.noget {
				continue
			}

			//这里处理,如果一条Fare记录已经存在Shopping成功的Routine,那么不再到远程获取没成功的Routine.
			comeback := true
			for _, mb := range ri.ListMB {
				if _, ok := mid[mb.ID]; !ok {
					comeback = false //还可以再提高：如果同航线低舱位有了,不同再获取高舱位航班
					break
				}
			}
			if comeback {
				continue
			}

			DepartStation := ri.Routine[:3]
			ArriveStation := ri.Routine[len(ri.Routine)-3:]
			Airline := ri.ListMB[0].AirInc //这里绝大多数是正确的,如果航线中存在不同航司,也会是同集团的.

			if _, ok := hadget[DepartStation+Airline+ArriveStation]; ok { //这种组合只是因为远程会自动返回多种中转
				continue //还可以再提高：如果同航线低舱位有了,不同再获取高舱位航班
			} else {
				hadget[DepartStation+Airline+ArriveStation] = struct{}{}
			}

			if getflight, ok := getData[DepartStation+ArriveStation]; ok {
				if Airline1E[Airline] {
					getflight.ListAirline1E = append(getflight.ListAirline1E, Airline)
				} else {
					getflight.ListAirline1B = append(getflight.ListAirline1B, Airline)
				}
			} else {
				getflight = &getFlight{
					Segment:       ri.Segment,
					TravelDate:    ri.TravelDate,
					ListAirline1E: []string{},
					ListAirline1B: []string{},
					c_fjson:       make(chan *cacheflight.FlightJSON, 2)}

				if Airline1E[Airline] {
					getflight.ListAirline1E = append(getflight.ListAirline1E, Airline)
				} else {
					getflight.ListAirline1B = append(getflight.ListAirline1B, Airline)
				}

				getData[DepartStation+ArriveStation] = getflight
			}
		}

		if len(getData) == 0 {
			goto No_getData //直接跳过,这样减少步骤,会快很多
		}

		for DepartArrive, Get := range getData {
			if len(Get.ListAirline1E) > 0 {
				go outsideapi.SearchFlightJSON_TO_V2(DepartArrive[:3], DepartArrive[3:], Get.TravelDate, strings.Join(Get.ListAirline1E, " "), "1E", Get.c_fjson)
			} else {
				Get.c_fjson <- nullFlightJSON
			}

			if len(Get.ListAirline1B) > 0 {
				go outsideapi.SearchFlightJSON_TO_V2(DepartArrive[:3], DepartArrive[3:], Get.TravelDate, strings.Join(Get.ListAirline1B, " "), "1B", Get.c_fjson)
			} else {
				Get.c_fjson <- nullFlightJSON
			}
		}

		qfi_fore_web = make([]cacheflight.FlightJSON, lenSeg, lenSeg)
		for i := range qfi_fore_web {
			qfi_fore_web[i] = cacheflight.FlightJSON{Route: make([]*cacheflight.RoutineInfoStruct, 0, 100)}
		}

		for _, Get := range getData {
			outout := <-Get.c_fjson
			if outout.Route != nil {
				if Get.Segment == 1 {
					qfi_fore_web[0].Route = append(qfi_fore_web[0].Route, outout.Route...)
				} else {
					qfi_fore_web[1].Route = append(qfi_fore_web[1].Route, outout.Route...)
				}
			}

			outout = <-Get.c_fjson
			if outout.Route != nil {
				if Get.Segment == 1 {
					qfi_fore_web[0].Route = append(qfi_fore_web[0].Route, outout.Route...)
				} else {
					qfi_fore_web[1].Route = append(qfi_fore_web[1].Route, outout.Route...)
				}
			}
		}

		for i := range rout { //这里只能假设远程航班并未返回其他有航司航班了！(为了检测Routine时就不匹配了)
			cycle = MainBillMatchingLegs(af, DealID, cycle, qfi_fore_web, mbris, rout[i].Segment, p2pi.DeviationRate, p2pi.ConnMinutes, p2pi.ShowBCC)
		}

	No_getData:
		if p2pi.Debug {
			if len(getData) > 0 {
				writeLog(dc, DealID, rmb, qfi_fore, qfi_fore_web)
			} else {
				writeLog(dc, DealID, rmb, qfi_fore, nil)
			}
		}
	}

	if af.Agency == "FB" {
		//这里合并ri.MM && ri.MMBCC,合并是未了减少BCC无效的输出,并排列MidJourney的价格.
		MergeMiddleMatch(mbris, lenSeg)
	}

	done, _ := InterJourney(mbris, lenSeg, true, thePrefixSuffix) //获取匹配完整的航线

	if quickout != nil {
		quickout <- done
	} else {
		shoppingout <- ToShopping(p2pi, rout, rmb, theTrip, thePrefixSuffix, done)
	}
}

//专用于KeyCache的shopping（这里），最后传递一个quickout，和shoppingout出去
func Point2Point_V4(
	QuickShopping bool, //是否快速票单
	dc *DebugControl, //写Debug日志
	DealID int, //处理序号,用于多次Shopping接驳时处理
	p2pi *Point2Point_In, //查询条件
	rout []*mysqlop.Routine, //rout是真实取获取的行段
	theTrip bool, //加入前续票单的要求(主票单Trip=1),主要用于控制票单Trip出票组合
	thePrefixSuffix bool, //加入前后续票单的要求(前后续票单Airline相同),主要用于控制Airline出票组合
	af *cachestation.Agencies, //供应商资料
	quickout chan []*MB2RouteInfo,
	shoppingout chan *Point2Point_Output) {

	//#TODO 获取缓存是在这里处理。在这里过滤调

	//如果没传route的话，则需要通过传入查询的p2pi.Flight 来进行数据的查询合并
	if rout == nil {
		rout = FlightLegs2Routine(p2pi.Flight)
	}

	var (
		rmb      mysqlop.ResultMainBill   //查询的航班结果集
		qfi_fore []cacheflight.FlightJSON //航班动态数据(缓存)[rout=Go/Back]
		mbris    []*MB2RouteInfo          //MainBill Routine Info
		cycle    int                      //用于航班编号使用
		backdate string
	)

	if len(rout) == 2 {
		backdate = rout[1].TravelDate
	}

	var bc *cacheflight.BlockCache
	var err error
	if af.Agency == "SearchOne" {
		//这里是1E的数据
		bc, err = getBlockCache(af.Agency, rout[0].DepartCounty, rout[0].ArriveCounty, rout[0].TravelDate, backdate, QuickShopping || p2pi.Days > 1)
	} else {

		fmt.Println("Point2Point_V4 --- 准备去调用getBlockCache")
		bc, err = getBlockCache(af.Agency, []string{p2pi.Flight[0].DepartStation}, []string{p2pi.Flight[0].ArriveStation}, rout[0].TravelDate, backdate, QuickShopping || p2pi.Days > 1)

	}
	if err != nil || bc == nil {
		if quickout != nil {
			quickout <- []*MB2RouteInfo{}
		} else {

			//已经通过上述的方法拿到数据。接下来在这里返回出去
			shoppingout <- &Point2Point_Output{
				Result:  100,
				Segment: make([]*Point2Point_RoutineInfo2Segment, 0),
				Fare:    make([]*FareInfo, 0)}
		}
		return
	}

	rmb = bc.Fares
	qfi_fore = bc.Flights

	//fmt.Println("bc.Fares", len(bc.Fares.Mainbill))

	tmpi := DealID * 1000000000
	for i, mb := range rmb.Mainbill {
		mb.ID = tmpi + i
	}

	if p2pi.Debug && dc != nil {
		writeLog(dc, DealID, rmb, qfi_fore, nil)
	}

	mbris = MainBill2RouteInfo(af, rmb, rout) //这里时简单的Routine分组

	for i := range rout { //i+1 == rout[i].Segment
		cycle = MainBillMatchingLegs(af, DealID, cycle, qfi_fore, mbris, rout[i].Segment, p2pi.DeviationRate, p2pi.ConnMinutes, p2pi.ShowBCC)
	}

	//for _, ri := range mbris {
	//	fmt.Println("mbris", ri.Routine, len(ri.MM))
	//}

	if quickout != nil {
		quickout <- mbris
	} else {
		shoppingout <- ToShopping(p2pi, rout, rmb, theTrip, thePrefixSuffix, mbris)
	}
}

/***2张出票接驳处理过程函数***/

//合并Journey Info
func MergeJourney_V1(
	p2pi *Point2Point_In, //查询条件,只用于判断中转时间
	output1 *Point2Point_Output, //结果集
	output2 *Point2Point_Output) (
	output JourneyInfoList) {

	if len(output1.Journey) == 0 || len(output2.Journey) == 0 {
		return []*JourneyInfo{}
	}

	//m_ris用于航班快速查找接驳
	m_ris := make(map[string]*cacheflight.RoutineInfoStruct, len(output1.Segment)+len(output2.Segment))
	for _, ris := range output1.Segment {
		m_ris[ris.SC] = ris.RoutineInfoStruct
	}
	for _, ris := range output2.Segment {
		m_ris[ris.SC] = ris.RoutineInfoStruct
	}

	//合并前后续票单
	var (
		ris1, ris2, ris3, ris4 *cacheflight.RoutineInfoStruct
		jt1, jt2               *JourneyTicketInfo
	)

	output = make([]*JourneyInfo, 0, 100)
	Trip := len(output1.Journey[0].JourneySegment)

	for _, foreJourney := range output1.Journey {
		for _, backJourney := range output2.Journey {
			//以下语句存在是因为ris3 = m_ris[backJourney.JourneySegment[1].SC]溢出
			//这种情况应该是中转地(多个)划分后在SegmentCode方面存在小误差,或者是偶然内存错误.
			if len(foreJourney.JourneySegment) != Trip ||
				len(backJourney.JourneySegment) != Trip {
				continue
			}

			//现在的情况为:每份foreJourney/backJourney都有2个JourneySegment完成双程
			ris1 = m_ris[foreJourney.JourneySegment[0].SC]
			ris2 = m_ris[backJourney.JourneySegment[0].SC]

			len1 := len(ris1.FI[0].Legs)

			if ris1.FI[0].Legs[len1-1].AS != ris2.FI[0].Legs[0].DS {
				continue //现在中转地是多的,出发/到达是单的.
			}

			if ris1.FI[0].Legs[len1-1].ASD > ris2.FI[0].Legs[0].DSD ||
				ris1.FI[0].Legs[len1-1].ASD == ris2.FI[0].Legs[0].DSD &&
					ris1.FI[0].Legs[len1-1].AST >= ris2.FI[0].Legs[0].DST ||
				//转机时间已经超过1440,但因为多Agency存在,所以不可以break
				ris1.FI[0].Legs[len1-1].ASD < ris2.FI[0].Legs[0].DSD &&
					ris1.FI[0].Legs[len1-1].AST < ris2.FI[0].Legs[0].DST {
				continue //时间接驳不上的
			}

			var js []*JourneySegmentInfo
			var jt []*JourneyTicketInfo

			if Trip > 1 {
				ris3 = m_ris[backJourney.JourneySegment[1].SC]
				ris4 = m_ris[foreJourney.JourneySegment[1].SC]
				len3 := len(ris3.FI[0].Legs)

				if ris3.FI[0].Legs[len3-1].ASD > ris4.FI[0].Legs[0].DSD ||
					ris3.FI[0].Legs[len3-1].ASD == ris4.FI[0].Legs[0].DSD &&
						ris3.FI[0].Legs[len3-1].AST >= ris4.FI[0].Legs[0].DST ||
					//转机时间已经超过1440,但因为多Agency存在,所以不可以break
					ris3.FI[0].Legs[len3-1].ASD < ris4.FI[0].Legs[0].DSD &&
						ris3.FI[0].Legs[len3-1].AST < ris4.FI[0].Legs[0].DST {
					continue //时间接驳不上的
				}

				_, CMs, _, CanConnect := CanConnectTime(ris1.FI[0].Legs[len1-1], ris2.FI[0].Legs[0])

				//fmt.Println("1-2: ", ris1.FI[0].Legs[len1-1].DS, ris1.FI[0].Legs[len1-1].AS, ris1.FI[0].Legs[len1-1].DSD, ris1.FI[0].Legs[len1-1].AST, ris2.FI[0].Legs[0].DS, ris2.FI[0].Legs[0].AS, ris2.FI[0].Legs[0].DSD, ris2.FI[0].Legs[0].DST, CanConnect, CMs, MCT, p2pi.ConnMinutes)

				if !CanConnect || CMs > p2pi.ConnMinutes {
					continue
				}

				_, CMs, _, CanConnect = CanConnectTime(ris3.FI[0].Legs[len3-1], ris4.FI[0].Legs[0])

				//fmt.Println("3-4: ", ris3.FI[0].Legs[len3-1].DS, ris3.FI[0].Legs[len3-1].AS, ris3.FI[0].Legs[len3-1].DSD, ris3.FI[0].Legs[len3-1].AST, ris4.FI[0].Legs[0].DS, ris4.FI[0].Legs[0].AS, ris4.FI[0].Legs[0].DSD, ris4.FI[0].Legs[0].DST, CanConnect, CMs, MCT, p2pi.ConnMinutes)

				if !CanConnect || CMs > p2pi.ConnMinutes {
					continue
				}

				//foreJourney.JourneySegment[0].NO = "1"
				backJourney.JourneySegment[0].NO = "2"
				backJourney.JourneySegment[1].NO = "3"
				foreJourney.JourneySegment[1].NO = "4"

				backJourney.JourneySegment[0].TID = "2"
				backJourney.JourneySegment[1].TID = "2"
				foreJourney.JourneySegment[1].TID = "1"

				js = []*JourneySegmentInfo{foreJourney.JourneySegment[0],
					backJourney.JourneySegment[0],
					backJourney.JourneySegment[1],
					foreJourney.JourneySegment[1]}

			} else { //Trip=1 单程
				_, CMs, _, CanConnect := CanConnectTime(ris1.FI[0].Legs[len1-1], ris2.FI[0].Legs[0])
				if !CanConnect || CMs > p2pi.ConnMinutes {
					continue
				}
				backJourney.JourneySegment[0].NO = "2"
				backJourney.JourneySegment[0].TID = "2"

				js = []*JourneySegmentInfo{foreJourney.JourneySegment[0],
					backJourney.JourneySegment[0]}
			}

			jt1 = foreJourney.JourneyTicket[0]
			jt2 = backJourney.JourneyTicket[0]
			jt2.TicketId = "2"

			jt = []*JourneyTicketInfo{jt1, jt2}

			tax1, _ := strconv.Atoi(foreJourney.Tax)
			tax2, _ := strconv.Atoi(backJourney.Tax)
			customer := foreJourney.Customer
			if customer == "" {
				customer = backJourney.Customer
			}

			output = append(output, &JourneyInfo{
				Customer:         customer,
				Status:           foreJourney.Status + backJourney.Status,
				Currency:         foreJourney.Currency,
				BasePrice:        foreJourney.BasePrice + backJourney.BasePrice,
				Tax:              strconv.Itoa(tax1 + tax2),
				TotalPrice:       foreJourney.TotalPrice + backJourney.TotalPrice,
				SourceBasePrice:  foreJourney.SourceBasePrice + backJourney.SourceBasePrice,
				SourceTotalPrice: foreJourney.SourceTotalPrice + backJourney.SourceTotalPrice,
				Bestbuy:          foreJourney.Bestbuy,
				JourneySegment:   js,
				JourneyTicket:    jt,
				Mid_journey:      backJourney.Mid_journey})
		}
	}

	return
}

//把各子shopping结果合并导致的相同航班信息合并(因为Fare已经是多组的)
func MergeSegmentFlight(shoppingout *Point2Point_Output) {
	if shoppingout.Result != 1 && shoppingout.Result != 2 {
		return
	}

	hasc := make(map[string][]*Point2Point_RoutineInfo2Segment, len(shoppingout.Segment))
	for _, sc := range shoppingout.Segment { //这里没有考虑到供应商问题,比较前端限制了.
		if msc, ok := hasc[sc.R+sc.RFN+sc.FI[0].Legs[0].DSD]; ok {
			hasc[sc.R+sc.RFN+sc.FI[0].Legs[0].DSD] = append(msc, sc)
		} else {
			hasc[sc.R+sc.RFN+sc.FI[0].Legs[0].DSD] = []*Point2Point_RoutineInfo2Segment{sc}
		}
	}

	for _, msc := range hasc {
		if len(msc) > 1 {
			for _, sc := range msc[1:] {
				for _, journey := range shoppingout.Journey {
					for _, jsi := range journey.JourneySegment {
						if jsi.SC == sc.SC {
							jsi.SC = msc[0].SC
						}
					}
				}
			}
		}
	}

	shoppingout.Segment = shoppingout.Segment[:0]
	for _, sc := range hasc {
		shoppingout.Segment = append(shoppingout.Segment, sc[0])
	}

	//过滤多余的(因为获取前面低价格)
	msc := make(map[string]*Point2Point_RoutineInfo2Segment, len(shoppingout.Segment))
	for _, sc := range shoppingout.Segment {
		msc[sc.SC] = sc
	}

	hafc := make(map[string]*FareInfo, len(shoppingout.Fare))
	for _, fc := range shoppingout.Fare {
		hafc[fc.FareCode] = fc
	}

	donesc := make(map[string]*Point2Point_RoutineInfo2Segment, len(shoppingout.Segment))
	donefc := make(map[string]*FareInfo, len(shoppingout.Fare))

	for _, journey := range shoppingout.Journey {
		for _, js := range journey.JourneySegment {
			donesc[js.SC] = msc[js.SC]
			donefc[js.FC] = hafc[js.FC]
		}
	}

	shoppingout.Segment = shoppingout.Segment[:0]
	for _, sc := range msc {
		shoppingout.Segment = append(shoppingout.Segment, sc)
	}

	shoppingout.Fare = shoppingout.Fare[:0]
	for _, fc := range donefc {
		shoppingout.Fare = append(shoppingout.Fare, fc)
	}
}

//########################################################
func MainBill2RouteInfo_V2(
	af *cachestation.Agencies, //供应商平台
	rmbs []mysqlop.ListMainBill, //静态Fare
	rout []*mysqlop.Routine) ( //查询条件
	mbris []*MB2RouteInfo) {

	mbris = make([]*MB2RouteInfo, 0, 50)
	hadRis := make(map[string]*MB2RouteInfo, 30)

	getMutilRoutine := func(routesMutil string) []string {
		if len(routesMutil) <= 10 {
			return []string{routesMutil}
		}

		rets := []string{}

		for _, routes := range strings.Split(routesMutil, "$") {
			srout := strings.Split(routes, "-")
			if len(srout) == 3 {
				for _, airline := range strings.Split(srout[1], " ") {
					rets = append(rets, srout[0]+"-"+airline+"-"+srout[2])
				}
			} else if len(srout) == 5 { //这里缺少处理多个中转的情况,他们是适用空格分隔
				for _, conn := range strings.Split(srout[2], " ") {
					for _, airline1 := range strings.Split(srout[1], " ") {
						for _, airline2 := range strings.Split(srout[3], " ") {
							rets = append(rets, srout[0]+"-"+airline1+"-"+conn+"-"+airline2+"-"+srout[4])
						}
					}
				}
			}
		}

		return rets
	}

	for seg, rmb := range rmbs {
		for _, mb := range rmb {
			if mb.Agency != af.ShowShoppingName && !strings.HasPrefix(mb.BillID, af.BillPrefix) {
				continue //过滤掉不同供应商的数据
			}

			//routine = mb.Routine //mb.Routine可以是多个的KUL-KA CX-HKG-3A-ZYK
			for _, routine := range getMutilRoutine(mb.Routine) { //这里必须先适用$划分多条航线
				k := routine + strconv.Itoa(mb.Trip) + mb.Agency + mb.PCC
				if ri, ok := hadRis[k]; ok {
					ri.ListMB = append(ri.ListMB, mb)
				} else {
					ri = &MB2RouteInfo{
						Routine:    routine,
						Segment:    seg + 1,
						TravelDate: rout[seg].TravelDate,
						Trip:       mb.Trip,
						Agency:     mb.Agency,
						PCC:        mb.PCC,
						ListMB:     append(make([]*mysqlop.MainBill, 0, 20), mb),
						MM:         make([]*MiddleMatch, 0, 30),
						MMBCC:      make([]*MiddleMatch, 0, 10),
						af:         af,
					}
					mbris = append(mbris, ri)
					hadRis[k] = ri
				}
			}
		}
	}
	return
}

//合并Journey Info
func MergeJourney_V2(
	p2pi *Point2Point_In, //查询条件,只用于判断中转时间
	output0, output1, output2, output3 *Point2Point_Output) (
	output *Point2Point_Output) { //必须控制最低价

	output = &Point2Point_Output{
		Result:  2,
		Segment: []*Point2Point_RoutineInfo2Segment{},
		Fare:    []*FareInfo{},
		Journey: []*JourneyInfo{}}

	if (len(output0.Journey) == 0 && len(output1.Journey) == 0) ||
		(len(output2.Journey) == 0 && len(output3.Journey) == 0) {
		return output
	}

	//m_ris用于航班快速查找接驳
	m_ris := make(map[string]*Point2Point_RoutineInfo2Segment, len(output0.Segment)+len(output1.Segment)+len(output2.Segment)+len(output3.Segment))
	for _, out := range []*Point2Point_Output{output0, output1, output2, output3} {
		for _, ris := range out.Segment {
			m_ris[ris.SC] = ris
		}
	}

	m_fc := make(map[string]*FareInfo, len(output0.Fare)+len(output1.Fare)+len(output2.Fare)+len(output3.Fare))
	for _, out := range []*Point2Point_Output{output0, output1, output2, output3} {
		for _, fare := range out.Fare {
			m_fc[fare.FareCode] = fare
		}
	}

	use_SC := make(map[string]struct{}, len(m_ris))
	use_FC := make(map[string]struct{}, len(m_fc))

	output.Journey = make([]*JourneyInfo, 0, len(output0.Journey)+len(output1.Journey))
	Trip := len(output0.Journey[0].JourneySegment) //JourneySegment是去程和回程列在一起

	notLink := func(ris0, ris1 *cacheflight.RoutineInfoStruct) bool {
		len0 := len(ris0.FI[0].Legs)

		if ris0.FI[0].Legs[len0-1].AS != ris1.FI[0].Legs[0].DS {
			return true //现在中转地是多的,出发/到达是单的.
		}

		if ris0.FI[0].Legs[len0-1].ASD > ris1.FI[0].Legs[0].DSD ||
			ris0.FI[0].Legs[len0-1].ASD == ris1.FI[0].Legs[0].DSD &&
				ris0.FI[0].Legs[len0-1].AST >= ris1.FI[0].Legs[0].DST ||
			//转机时间已经超过1440,但因为多Agency存在,所以不可以break
			ris0.FI[0].Legs[len0-1].ASD < ris1.FI[0].Legs[0].DSD &&
				ris0.FI[0].Legs[len0-1].AST < ris1.FI[0].Legs[0].DST {
			return true //时间接驳不上的
		}

		return false
	}

	for _, foreJourney := range append(output0.Journey, output1.Journey...) {
		for _, backJourney := range append(output2.Journey, output3.Journey...) {

			ris0, ok0 := m_ris[foreJourney.JourneySegment[0].SC]
			ris1, ok1 := m_ris[backJourney.JourneySegment[0].SC]
			if !ok0 || !ok1 || notLink(ris0.RoutineInfoStruct, ris1.RoutineInfoStruct) {
				continue
			}

			var js []*JourneySegmentInfo
			var jt []*JourneyTicketInfo

			if Trip > 1 {
				ris2, ok2 := m_ris[backJourney.JourneySegment[1].SC]
				ris3, ok3 := m_ris[foreJourney.JourneySegment[1].SC]
				if !ok2 || !ok3 || notLink(ris2.RoutineInfoStruct, ris3.RoutineInfoStruct) {
					continue
				}

				_, CMs, _, CanConnect := CanConnectTime(ris0.FI[0].Legs[len(ris0.FI[0].Legs)-1], ris1.FI[0].Legs[0])
				if !CanConnect || CMs > p2pi.ConnMinutes {
					continue
				}

				_, CMs, _, CanConnect = CanConnectTime(ris2.FI[0].Legs[len(ris2.FI[0].Legs)-1], ris3.FI[0].Legs[0])
				if !CanConnect || CMs > p2pi.ConnMinutes {
					continue
				}

				//foreJourney.JourneySegment[0].NO = "1"
				backJourney.JourneySegment[0].NO = "2"
				backJourney.JourneySegment[1].NO = "3"
				foreJourney.JourneySegment[1].NO = "4"

				backJourney.JourneySegment[0].TID = "2"
				backJourney.JourneySegment[1].TID = "2"
				foreJourney.JourneySegment[1].TID = "1"

				js = []*JourneySegmentInfo{
					foreJourney.JourneySegment[0],
					backJourney.JourneySegment[0],
					backJourney.JourneySegment[1],
					foreJourney.JourneySegment[1]}

				use_SC[foreJourney.JourneySegment[0].SC] = struct{}{}
				use_SC[backJourney.JourneySegment[0].SC] = struct{}{}
				use_FC[foreJourney.JourneySegment[0].FC] = struct{}{}
				use_FC[backJourney.JourneySegment[0].FC] = struct{}{}

			} else { //Trip=1 单程
				_, CMs, _, CanConnect := CanConnectTime(ris0.FI[0].Legs[len(ris0.FI[0].Legs)-1], ris1.FI[0].Legs[0])
				if !CanConnect || CMs > p2pi.ConnMinutes {
					continue
				}

				backJourney.JourneySegment[0].NO = "2"
				backJourney.JourneySegment[0].TID = "2"

				js = []*JourneySegmentInfo{
					foreJourney.JourneySegment[0],
					backJourney.JourneySegment[0]}

				use_SC[foreJourney.JourneySegment[0].SC] = struct{}{}
				use_SC[foreJourney.JourneySegment[1].SC] = struct{}{}
				use_SC[backJourney.JourneySegment[0].SC] = struct{}{}
				use_SC[backJourney.JourneySegment[1].SC] = struct{}{}
				use_FC[foreJourney.JourneySegment[0].FC] = struct{}{}
				use_FC[foreJourney.JourneySegment[1].FC] = struct{}{}
				use_FC[backJourney.JourneySegment[0].FC] = struct{}{}
				use_FC[backJourney.JourneySegment[1].FC] = struct{}{}
			}

			backJourney.JourneyTicket[0].TicketId = "2"
			jt = []*JourneyTicketInfo{foreJourney.JourneyTicket[0], backJourney.JourneyTicket[0]}

			tax1, _ := strconv.Atoi(foreJourney.Tax)
			tax2, _ := strconv.Atoi(backJourney.Tax)
			customer := foreJourney.Customer
			if customer == "" {
				customer = backJourney.Customer
			}

			output.Journey = append(output.Journey, &JourneyInfo{
				Customer:         customer,
				Status:           foreJourney.Status + backJourney.Status,
				Currency:         foreJourney.Currency,
				BasePrice:        foreJourney.BasePrice + backJourney.BasePrice,
				Tax:              strconv.Itoa(tax1 + tax2),
				TotalPrice:       foreJourney.TotalPrice + backJourney.TotalPrice,
				SourceBasePrice:  foreJourney.SourceBasePrice + backJourney.SourceBasePrice,
				SourceTotalPrice: foreJourney.SourceTotalPrice + backJourney.SourceTotalPrice,
				Bestbuy:          foreJourney.Bestbuy,
				JourneySegment:   js,
				JourneyTicket:    jt,
				Mid_journey:      backJourney.Mid_journey})
		}
	}

	for k := range use_SC {
		if sc, ok := m_ris[k]; ok {
			output.Segment = append(output.Segment, sc)
		}
	}

	for k := range use_FC {
		if fc, ok := m_fc[k]; ok {
			output.Fare = append(output.Fare, fc)
		}
	}

	return
}

func ToShopping_V2(
	p2pi *Point2Point_In, //查询条件
	rout []*mysqlop.Routine, //rout是真实取获取的行段
	rmb mysqlop.ResultMainBill, //查询的航班结果集
	theTrip bool, //加入前续票单的要求(主票单Trip=1),主要用于控制票单Trip出票组合
	thePrefixSuffix bool, //加入前后续票单的要求(前后续票单Airline相同),主要用于控制Airline出票组合
	mbris []*MB2RouteInfo) *Point2Point_Output {

	var (
		lenSeg       = len(rout)         //rout是真实取获取的行段
		mid          map[int]struct{}    //Record MainBill ID
		mid_journey  []JMIList           //把各Segment-->Journey的中间结果保存
		p2p_out_json *Point2Point_Output //shopping out
	)

	if rmb.Mainbill == nil {
		for _, ris := range mbris {
			rmb.Mainbill = append(rmb.Mainbill, ris.ListMB...)
		}
	}

	mid = make(map[int]struct{}, len(rmb.Mainbill))
	p2p_out_json = &Point2Point_Output{
		Result:  2,
		Segment: make([]*Point2Point_RoutineInfo2Segment, 0, 200),
		Fare:    make([]*FareInfo, 0, len(rmb.Mainbill))}

	mid_journey = make([]JMIList, lenSeg, lenSeg)
	for i := range rout {
		mid_journey[i] = make([]*JourneyMiddleInfo, 0, 200)
	}

	for _, ri := range mbris { //处理Mid Journey!
		for _, mm := range ri.MM {
			p2p_out_json.Segment = append(p2p_out_json.Segment, mm.fjson)
			//len(doneID)==len(output)==len(fjson)
			mid_journey[ri.Segment-1] = append(mid_journey[ri.Segment-1], mm.output...) //Segment start in 1
			for _, n := range mm.doneID {                                               //登记需要使用的MainBill
				mid[n] = struct{}{}
			}
		}
	}

	//排列MiddleJourney价格
	for _, journey := range mid_journey { //没有中途数据直接返回
		if len(journey) == 0 {
			return &Point2Point_Output{
				Result:  2,
				Segment: []*Point2Point_RoutineInfo2Segment{},
				Fare:    []*FareInfo{}}
		}
	}

	for _, rid := range rmb.Mainbill { //添加Shopping.Fare节点
		if _, ok := mid[rid.ID]; ok {
			p2p_out_json.Fare = append(p2p_out_json.Fare,
				&FareInfo{
					FareCode: strconv.Itoa(rid.ID),
					MainBill: rid})
		}
	}

	p2p_out_json.Journey = MakingJourney(mid_journey, p2pi, rout, thePrefixSuffix)
	//fmt.Println("Journey Len", len(p2p_out_json.Journey), len(p2p_out_json.Segment), len(p2p_out_json.Fare)) ///////////

	JourneyAddTax(p2p_out_json.Journey, !theTrip && thePrefixSuffix)

	//排列价格及控制输出量
	if len(p2p_out_json.Journey) == 0 {
		if !p2pi.Debug {
			return &Point2Point_Output{ //票单合并时产生出错,所以只能在直接返回时处理
				Result:  2,
				Segment: []*Point2Point_RoutineInfo2Segment{},
				Fare:    []*FareInfo{}}
		} else {
			return p2p_out_json
		}
	} else {
		sort.Sort(p2p_out_json.Journey)
		if p2pi.MaxOutput >= 100 && len(p2p_out_json.Journey) > p2pi.MaxOutput {
			p2p_out_json.Journey = p2p_out_json.Journey[:p2pi.MaxOutput]
		}
		//MergeSegmentFlight(p2p_out_json) //这里出现是因为信息被重复使用
		return p2p_out_json
	}

}

func computeDate(rout []*mysqlop.Routine, Day, addOutDay, addInDay int) (string, string) {
	traveldate := rout[0].TravelDate
	if Day != 0 || addOutDay != 0 {
		td, _ := time.Parse("2006-01-02", traveldate)
		traveldate = td.AddDate(0, 0, Day+addOutDay).Format("2006-01-02")
	}

	backdate := ""
	if len(rout) == 2 {
		backdate = rout[1].TravelDate
		if Day != 0 || addInDay != 0 { //双程才有addInDay>0
			bd, _ := time.Parse("2006-01-02", backdate)
			backdate = bd.AddDate(0, 0, Day+addInDay).Format("2006-01-02")
		}
	}

	return traveldate, backdate
}

//这个是什么意思呢？
type CacheConnDay struct {
	addDays, addInDay, addOutDay, ticketCount int
}

var AnalysisDay = struct {
	mutex   sync.RWMutex
	routine map[string]*CacheConnDay
}{
	routine: make(map[string]*CacheConnDay, 10000),
}

func AnalysisConnDay(routine string, date string, ps bool) (int, bool) {
	//当非快速的组合供应商处理
	addLen := len(routine) - 17
	var leg [2]string
	if ps { //前续
		leg[0] = routine[:10]
		leg[1] = routine[7:17]
	} else { //后续时这里不好,因为2次转机问题.
		leg[0] = routine[0+addLen : 10+addLen]
		leg[1] = routine[7+addLen : 17+addLen]
	}

	c_leg := make(chan *cacheflight.FlightJSON, 1)

	getRout := func(fj *cacheflight.FlightJSON, rout string, ps, legOne bool) int {
		i := 0
		for ; i < len(fj.Route); i++ {
			if fj.Route[i].R == rout {
				break
			}
		}
		if i != len(fj.Route) || ps && legOne {
			return i //既是前续又是第1段
		}

		i = len(fj.Route) - 1
		for ; i >= 0; i-- {
			if len(fj.Route[i].R) == 17 &&
				(ps && !legOne && fj.Route[i].R[:10] == rout ||
					!ps && legOne && fj.Route[i].R[7:] == rout) {
				break
			}
		}

		if i < 0 {
			return len(fj.Route)
		}
		return i
	}

	QueryLegInfo(&cacheflight.RoutineService{
		Deal:           "QueryFore",
		DepartStation:  leg[0][:3],
		ConnectStation: []string{},
		ArriveStation:  leg[0][7:],
		TravelDate:     date},
		cachestation.PetchIP[leg[0][:3]], c_leg)

	fore := <-c_leg
	sort.Sort(fore.Route)
	m := getRout(fore, leg[0], ps, true)
	if m == len(fore.Route) || len(fore.Route) == 0 {
		return 0, false //什么都没办法确认的情况下,增加一天
	}

	legLen := len(fore.Route[m].FI[0].Legs)
	day := fore.Route[m].TDs + fore.Route[m].CDs
	if day > 0 {
		td, _ := time.Parse("2006-01-02", date)
		date = td.AddDate(0, 0, day).Format("2006-01-02")
	}

	QueryLegInfo(&cacheflight.RoutineService{
		Deal:           "QueryFore",
		DepartStation:  leg[1][:3],
		ConnectStation: []string{},
		ArriveStation:  leg[1][7:],
		TravelDate:     date},
		cachestation.PetchIP[leg[1][:3]], c_leg)

	back := <-c_leg
	if len(back.Route) == 0 {
		return day, day > 0 //不同一天,默认是对的,同一天的话,进入4票模式
	}
	sort.Sort(back.Route)
	n := getRout(back, leg[1], ps, false)
	if n == len(back.Route) {
		return day, false //进入4票模式(在加day的基础上)
	}

	if day > 0 {
		return day, true
	}

	if fore.Route[m].FI[0].Legs[legLen-1].AST <
		back.Route[n].FI[0].Legs[0].DST {
		return day, true //0,true
	} else {
		return day + 1, true //1,true
	}
}

func TwoTicketConnDay(ar *cachestation.AgRoutine, p2pi *Point2Point_In) (int, int, int, int) {

	addDays, addInDay, addOutDay, ticketCount := 0, 0, 0, 2
	okIn, okOut := false, false

	AnalysisDay.mutex.RLock()
	day, ok := AnalysisDay.routine[ar.Routine]
	AnalysisDay.mutex.RUnlock()
	if ok {
		return day.addDays, day.addInDay, day.addOutDay, day.ticketCount
	}

	addOutDay, okOut = AnalysisConnDay(ar.Routine, p2pi.Rout[0].TravelDate, ar.PS)
	addInDay, okIn = AnalysisConnDay(RedoRoutineMutil(ar.Routine), p2pi.Rout[1].TravelDate, !ar.PS)
	if !okOut && !okIn {
		if addOutDay == 0 {
			ticketCount = 4
			addDays = 1 //4票的Cache必须分配多1天
		}
	} else {
		if !okOut {
			if addOutDay == 0 {
				ticketCount = 4
				addDays = 1 //4票的Cache必须分配多1天
			} else {
				addOutDay++
			}
		}
		if !okIn {
			if addInDay == 0 {
				ticketCount = 4
				addDays = 1
			} else {
				addInDay++
			}
		}
	}

	AnalysisDay.mutex.Lock()
	AnalysisDay.routine[ar.Routine] = &CacheConnDay{addDays, addInDay, addOutDay, ticketCount}
	AnalysisDay.mutex.Unlock()
	return addDays, addInDay, addOutDay, ticketCount
}
