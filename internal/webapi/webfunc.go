package webapi

import (
	"bytes"
	"cacheflight"
	"cachestation"
	"compress/gzip"
	"encoding/json"
	"errorlog"
	"errors"
	//"fmt"
	"io/ioutil"
	"mysqlop"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

//同步票单数据（必须要sqlserver 以及 mysql都有）
func SynchronousData() {

	fares := mysqlop.GetAllFare()     //SqlServer
	b2fares := mysqlop.GetAllB2Fare() //MySQL
	if len(fares) == 0 || len(b2fares) == 0 {
		return
	}
	for b2k := range b2fares {
		if _, ok := fares[b2k]; !ok {
			for _, server := range mysqlop.ServiceSelect("B2FareDelete") {
				mysqlop.DeleteB2Fare(server, b2k)
			}
			mysqlop.DeleteMainBillData(b2k)
		}
	}

	for fk, fv := range fares {
		if b2v, ok := b2fares[fk]; !ok || fv.Sub(b2v).Minutes() > 10 {
			if mysqlop.GetMainBillData(fk) {
				for _, server := range mysqlop.ServiceSelect("B2FareReload") {
					mysqlop.ReloadB2Fare(server, fk)
				}
			}
		}
	}

}

//远程获取缓存航班数据
//#TODO 在查询直飞航班的时候调用到。。
//rs 要查询的航班信息；；   serverIP 对应的服务器地址111，112，117其中一个；   cfjs 这里指的是用来存取回调的数据
func QueryLegInfo(rs *cacheflight.RoutineService, serverIP string, cfjs chan *cacheflight.FlightJSON) {

	var fjs cacheflight.FlightJSON
	//重新定义了一个变量fjs。将我们最终的结果回调给cfjs。回传出去
	defer func() {
		cfjs <- &fjs
	}()

	if serverIP == "" {
		return
	}

	//通过上面接受到参数，将数据marshal转为byte。接着用bytes.NewBuffer(b)======>转成对应的body发到请求里面去
	b, err := json.Marshal(rs)
	if err != nil {
		return
	}
	body := bytes.NewBuffer([]byte(b))

	//对应的要调用的url地址，其实就是ks_basic里面的接口，放在三台服务器上。由对应的ip自动去选择那台。我们在这三台服务器上都已经开启了ks_basic这个服务
	//这就是所谓的分布式。其实可以理解和微服务的原理差不多
	s := "http://" + serverIP + ":9999/QueryFlightInfo"
	res, err := http.Post(s, "application/json;charset=utf-8", body)
	if err != nil {
		errorlog.WriteErrorLog("QueryLegInfo (1): " + err.Error())
		return
	}

	defer res.Body.Close()

	//结果拿到了 res  。。。。。 接着将res再经过转化，重新传出去外面
	gnr, err := gzip.NewReader(res.Body)
	if err != nil {
		errorlog.WriteErrorLog("QueryLegInfo (2): " + err.Error())
		return
	}
	defer gnr.Close()
	result, _ := ioutil.ReadAll(gnr)
	//将请求到的数据，重新转成fjs出去。再利用通道，转到cfjs回去
	if err := json.Unmarshal(result, &fjs); err != nil {
		errorlog.WriteErrorLog("QueryLegInfo (4): " + err.Error())
	}

}

//这里用在FlightTime的航线计数
func ShortRoutine_V2(routine string) string {

	route := routine[:3] + "-" + routine[7:10]

	for i := 14; i < len(routine); i += 7 {
		route += "-" + routine[i:i+3]
	}

	return route
}

//这里用于前续票单航线相同
func ShortRoutineMutil(routine string) string {
	sr := strings.Split(routine, "-")
	if len(sr) == 3 {
		return sr[0] + "-" + sr[2]
	} else if len(sr) == 5 {
		return sr[0] + "-" + sr[2] + "-" + sr[4]
	} else {
		return ""
	}
}


/**
输入：出发机场，城市，到达机场，城市，航司
输出:两个出发地目的地航司 组合 数组，

*/
func Get_DestSpriAirL(DepartStation, DepartCountry, ArriveStation, ArriveCountry, Airline string) ([]string, []string) {
	DestSpriAirL := make([]string, 0, 18)
	DestSpriAirL_V2 := make([]string, 0, 9)



	if Airline != "" {
		DestSpriAirL = append(DestSpriAirL, ArriveStation+DepartStation+Airline)  //CANBJSPK
		DestSpriAirL = append(DestSpriAirL, ArriveCountry+"*"+DepartStation+Airline) //CAN*BJSPK
		DestSpriAirL = append(DestSpriAirL, "***"+DepartStation+Airline)   //***CANPK
		DestSpriAirL = append(DestSpriAirL, ArriveStation+DepartCountry+"*"+Airline)
		DestSpriAirL = append(DestSpriAirL, ArriveCountry+"*"+DepartCountry+"*"+Airline)
		DestSpriAirL = append(DestSpriAirL, "***"+DepartCountry+"*"+Airline)
		DestSpriAirL = append(DestSpriAirL, ArriveStation+"***"+Airline)
		DestSpriAirL = append(DestSpriAirL, ArriveCountry+"****"+Airline)  //CAN****PK
		DestSpriAirL = append(DestSpriAirL, "******"+Airline)   //******PK
	} else {
		DestSpriAirL_V2 = append(DestSpriAirL_V2, ArriveStation+DepartStation)
		DestSpriAirL_V2 = append(DestSpriAirL_V2, ArriveCountry+"*"+DepartStation)
		DestSpriAirL_V2 = append(DestSpriAirL_V2, "***"+DepartStation)
		DestSpriAirL_V2 = append(DestSpriAirL_V2, ArriveStation+DepartCountry+"*")
		DestSpriAirL_V2 = append(DestSpriAirL_V2, ArriveCountry+"*"+DepartCountry+"*")
		DestSpriAirL_V2 = append(DestSpriAirL_V2, "***"+DepartCountry+"*")
		DestSpriAirL_V2 = append(DestSpriAirL_V2, ArriveStation+"***")
		DestSpriAirL_V2 = append(DestSpriAirL_V2, ArriveCountry+"****")
		DestSpriAirL_V2 = append(DestSpriAirL_V2, "******")
	}

	DestSpriAirL = append(DestSpriAirL, ArriveStation+DepartStation+"**")
	DestSpriAirL = append(DestSpriAirL, ArriveCountry+"*"+DepartStation+"**")
	DestSpriAirL = append(DestSpriAirL, "***"+DepartStation+"**")
	DestSpriAirL = append(DestSpriAirL, ArriveStation+DepartCountry+"***")
	DestSpriAirL = append(DestSpriAirL, ArriveCountry+"*"+DepartCountry+"***")
	DestSpriAirL = append(DestSpriAirL, "***"+DepartCountry+"***")
	DestSpriAirL = append(DestSpriAirL, ArriveStation+"*****")
	DestSpriAirL = append(DestSpriAirL, ArriveCountry+"******")
	DestSpriAirL = append(DestSpriAirL, "********")

	return DestSpriAirL, DestSpriAirL_V2
}

func Get_ExceptDestSpriAirL(DepartStation, DepartCountry, ArriveStation, ArriveCountry, Airline string) ([]string, []string) {
	DestSpriAirL := make([]string, 0, 8)
	DestSpriAirL_V2 := make([]string, 0, 4)

	if Airline != "" {
		DestSpriAirL = append(DestSpriAirL, ArriveStation+DepartStation+Airline)
		DestSpriAirL = append(DestSpriAirL, ArriveStation+DepartCountry+"*"+Airline)
		DestSpriAirL = append(DestSpriAirL, ArriveCountry+"*"+DepartStation+Airline)
		DestSpriAirL = append(DestSpriAirL, ArriveCountry+"*"+DepartCountry+"*"+Airline)
	} else {
		DestSpriAirL_V2 = append(DestSpriAirL_V2, ArriveStation+DepartStation)
		DestSpriAirL_V2 = append(DestSpriAirL_V2, ArriveStation+DepartCountry+"*")
		DestSpriAirL_V2 = append(DestSpriAirL_V2, ArriveCountry+"*"+DepartStation)
		DestSpriAirL_V2 = append(DestSpriAirL_V2, ArriveCountry+"*"+DepartCountry+"*")
	}

	DestSpriAirL = append(DestSpriAirL, ArriveStation+DepartStation+"**")
	DestSpriAirL = append(DestSpriAirL, ArriveStation+DepartCountry+"***")
	DestSpriAirL = append(DestSpriAirL, ArriveCountry+"*"+DepartStation+"**")
	DestSpriAirL = append(DestSpriAirL, ArriveCountry+"*"+DepartCountry+"***")

	return DestSpriAirL, DestSpriAirL_V2
}

//获取最适合的折扣计算
func F_DCPriceCompute(OfficeID string, Airline string, DepartStation string, ArriveStation string, TravelDate string,
	Trip int, GoorBack int /*不是从MB出来的GoorBack*/, CrossSeason string, GoodType string, price int, Berth string,
	GDS string, PCC string) (
	int, float64, float64, int, string, bool) {

	var (
		DestSpriAirL  []string
		DepartCountry string
		ArriveCountry string
		s_today       string
		today         time.Time
		Departuredate time.Time
		dc            [2]cachestation.LDestSpriAirL
		ok            bool
		days          int
		map_dsa       cachestation.LDestSpriAirL
		dca_array     []*cachestation.DiscountCodeAir
		model_2       []*cachestation.Model_2
		DiscountCodes []string
		low_price     int = 2000000
		tmp_price     int
		AgencyFee     float64
		EncourageFee  float64
		TicketFee     int
		AgencyRay     string
	)

	model_2, ok = cachestation.Model_3[OfficeID]
	for _, m2 := range model_2 {
		if m2.DataSource == GDS && (m2.PCC == "" || m2.PCC == PCC) {
			DiscountCodes = append(DiscountCodes, m2.DiscountCode)
		}
	}

	if !ok || len(DiscountCodes) == 0 {
		return price, 0, 0, 0, "", false
	}

	if county, ok := cachestation.County[DepartStation]; ok {
		DepartStation = county.City
	}

	if county, ok := cachestation.County[ArriveStation]; ok {
		ArriveStation = county.City
	}

	if DepartCountry, ok = cachestation.CityCountry[DepartStation]; !ok {
		return price, 0, 0, 0, "", false
	}

	if ArriveCountry, ok = cachestation.CityCountry[ArriveStation]; !ok {
		return price, 0, 0, 0, "", false
	}

	s_today = errorlog.Today()
	today, _ = time.Parse("2006-01-02", s_today)
	Departuredate, _ = time.Parse("2006-01-02", TravelDate)
	days = int(Departuredate.Sub(today).Hours() / 24)

	for _, DiscountCode := range DiscountCodes {
		dc_lowprice := 2000000 //每一折扣代码进入计算一次

		cachestation.DCAtypeB.M.RLock()
		dc, ok = cachestation.DCAtypeB.CacheDiscountCodeAir[DiscountCode]
		cachestation.DCAtypeB.M.RUnlock()

		if ok { //DiscountCode原来为OfficeID
			if GoodType == "INTLAIR" { //国际
				map_dsa = dc[0] //.LDestSpriAirL
			} else { //AIR 国内
				map_dsa = dc[1] //.LDestSpriAirL
			}
		} else {
			continue
		}

		if map_dsa.DestSpriAirL == nil {
			continue
		}

		DestSpriAirL, _ = Get_DestSpriAirL(DepartStation, DepartCountry, ArriveStation, ArriveCountry, Airline)

		for _, dsa := range DestSpriAirL {

			//map_dsa.M.RLock()
			dca_array, ok = map_dsa.DestSpriAirL[dsa]
			//map_dsa.M.RUnlock()

			if ok {
				for _, dca := range dca_array {
					if dca.DiscountCode == DiscountCode &&
						dca.AheadDays <= days &&
						dca.Trip == Trip &&
						dca.GoorBack == GoorBack &&
						dca.MinApplyPrice <= price &&
						price <= dca.MaxApplyPrice &&
						dca.ReserveFirstDate <= s_today &&
						dca.ReserveLastDate >= s_today &&
						dca.TravelFirstDate <= TravelDate &&
						dca.TravelLastDate >= TravelDate &&
						(dca.Berth == "" || strings.Contains(Berth, dca.Berth)) &&
						(CrossSeason == "A" || dca.CrossSeason == CrossSeason) &&
						(dca.CountryTeam == "" || !strings.Contains(dca.CountryTeam, DepartCountry)) &&
						(dca.DestCounTeam == "" || !strings.Contains(dca.DestCounTeam, ArriveCountry)) {

						tmp_price = price - int(float64(price)*(dca.AgencyFee+dca.EncourageFee)/100) + dca.TicketFee
						if low_price > tmp_price {
							low_price = tmp_price
							AgencyFee = dca.AgencyFee
							EncourageFee = dca.EncourageFee
							TicketFee = dca.TicketFee
							AgencyRay = dca.OfficeID
						}

						dc_lowprice = tmp_price
						break //DiscountCode的每个DestSpriAirL都包含多个计算记录(包含各舱位及错误录入)
					}
				}
			}

			if dc_lowprice != 2000000 {
				break
			}
		}

		if dc_lowprice != 2000000 {
			break
		}
	}

	if low_price == 2000000 {
		return price, 0, 0, 0, "", false
	} else {
		return low_price, AgencyFee, EncourageFee, TicketFee, AgencyRay, true
	}
}

//把航线倒过来(多中转多航司*票单数据的航班路线)//多转目的地在其它地方分解了
func RedoRoutineMutil(routine string) string {
	sr := strings.Split(routine, "-")

	for from, to := 0, len(sr)-1; from < to; from, to = from+1, to-1 {
		sr[from], sr[to] = sr[to], sr[from]
	}

	return strings.Join(sr, "-")
}

//判断Routine是否存在相同(可以有航司也可以没航司)//过时函数
func HasSameRoutine(r1, r2 string) bool {
	r1Len := len(r1)
	r2Len := len(r2)

	if r1Len < 7 || r2Len < 7 ||
		r1[:3] != r2[:3] ||
		r1[r1Len-3:] != r2[r2Len-3:] {
		return false
	}

	startIndex1 := 4
	startIndex2 := 4
	pos1 := strings.Index(r1[startIndex1:], "-")
	pos2 := strings.Index(r2[startIndex2:], "-")

	for {
		if pos1 == -1 || pos2 == -1 {
			if pos1 != pos2 {
				return false
			} else {
				return true
			}
		}

		ss1 := strings.Split(r1[startIndex1:startIndex1+pos1], " ")
		ss2 := strings.Split(r2[startIndex2:startIndex2+pos2], " ")

		has := false
		for _, s1 := range ss1 {
			for _, s2 := range ss2 {
				if s1 == s2 || s1 == "*" || s2 == "*" {
					has = true
					break
				}
			}
			if has {
				break
			}
		}

		if !has {
			return false
		}

		startIndex1 += pos1 + 1
		startIndex2 += pos2 + 1
		pos1 = strings.Index(r1[startIndex1:], "-")
		pos2 = strings.Index(r2[startIndex2:], "-")
	}
	return true
}

//获取用户Shopping时的航班路线,以备缓存(是否将用户查询过的线路缓存起来，下次可以从缓存中获取)
func JourneyLine(ppi *Point2Point_In) string {
	var ret string

	//CANBJS2018-08-23-DSICIKDMV2018-08-29-
	for _, flight := range ppi.Flight {
		ret += flight.DepartStation + flight.ArriveStation + flight.TravelDate + "-"
	}

	//CZHUMU
	for _, al := range ppi.Airline {
		ret += al
	}
	//CANBJS2018-08-23-DSICIKDMV2018-08-29-CZHU-222Y  这是一个demo

	ret += "-" + strconv.Itoa(ppi.ChangeFlag) + strconv.Itoa(ppi.BackFlag) + strconv.Itoa(ppi.BaggageFlag) + ppi.BerthType

	return ret
}

//计算航班接驳
func CanConnectTime(first *cacheflight.FlightInfoStruct, second *cacheflight.FlightInfoStruct) (int, int, int, bool) {

	d1, _ := time.Parse("2006-01-02", first.ASD)  //第一段航班的到达日期  如2018-09-03
	d2, _ := time.Parse("2006-01-02", second.DSD) //第二段航班的出发日期 如2018-09-05
	t1 := cacheflight.F_Time2Int(first.AST)       //第一段航班的到达具体时间  最终是转化为多少分钟
	t2 := cacheflight.F_Time2Int(second.DST)      //第二段航班的出发日期 最终是转化为多少分钟

	CDs := int(d2.Sub(d1).Hours() / 24) //转机天数
	CMs := CDs*1440 + t2 - t1           //转机时长

	if CMs < 20 || (CMs < 60 && first.AD != second.AD) {
		return CDs, CMs, 60, false
	}

	if CMs >= 60 && first.AD == second.AD { //这里还可以处理同航司集团及航空联盟问题
		return CDs, CMs, 60, true
	}

	if CMs >= 240 {
		return CDs, CMs, 240, true
	}

	MCT := cacheflight.F_GetMCT(first.AS, first.AD, second.AD, first.ACT, second.ACT,
		first.DTerm, second.ATerm, first.DS, second.AS, first.FN, second.FN)

	return CDs, CMs, MCT, CMs >= MCT
}

//匹配航司是否符合
func MatchingAirline(Legs []*cacheflight.FlightInfoStruct, Airline string) bool {
	for _, leg := range Legs {
		if leg.AD != Airline {
			return false
		}
	}
	return true
}

//匹配航司联盟是否符合
func MatchingAlliance(Legs []*cacheflight.FlightInfoStruct, AllianceIndex int) bool {
	for _, leg := range Legs {
		if !cachestation.Alliance[AllianceIndex][leg.AD] {
			return false
		}
	}
	return true
}

//减少不符合条件的Routine
func ReduceRout(fjson *cacheflight.FlightJSON, ShareAirline string, Airline string, AllianceIndex int, TwoLeg bool) *cacheflight.FlightJSON {
	routLen := len(fjson.Route)

	for i := 0; i < routLen; i++ {
		if ShareAirline == "A" && fjson.Route[i].SA == "B" ||
			TwoLeg && Airline != "" && //下面在真实业务中到在3段航段的2连段就已经有有效航司
				Airline != fjson.Route[i].FI[0].Legs[0].AD && Airline != fjson.Route[i].FI[0].Legs[1].AD ||
			AllianceIndex < 99 && !MatchingAlliance(fjson.Route[i].FI[0].Legs, AllianceIndex) {
			fjson.Route[i], fjson.Route[routLen-1] = fjson.Route[routLen-1], fjson.Route[i]
			routLen--
			i--
		}
	}
	if routLen != len(fjson.Route) && len(fjson.Route) > 0 {
		fjson.Route = fjson.Route[:routLen]
		sort.Sort(fjson.Route)
	}
	return fjson
}

/////////////////////////////////////////////////////////////
/////////*这里就是进行Debug的地方了。进行调试。下面是其方法*///////////
/////////////////////////////////////////////////////////////
type DebugControl struct {
	journeyLinePath string     //航线路径
	file            *os.File   //文件
	mutex           sync.Mutex //加锁
}

func (this *DebugControl) Init(JourneyLine string) {
	this.journeyLinePath = "/home/wds/ks_debug/" +
		strings.Replace(strings.Replace(errorlog.DateTime()+"-"+JourneyLine, " ", "", -1), ":", "", -1) + ".txt"
	os.Remove(this.journeyLinePath)

}

func (this *DebugControl) Open() {
	this.mutex.Lock()
	this.file, _ = os.OpenFile(this.journeyLinePath, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0660)
	this.file.WriteString("==================================\n\n")
}

func (this *DebugControl) Close() {
	this.file.Close()
	this.mutex.Unlock()
}

//往日志写入内容
func (this *DebugControl) AddInfo(title string, content string) {
	this.file.WriteString(title)
	this.file.WriteString("\n\n")
	this.file.WriteString(content)
	this.file.WriteString("\n\n")
}

/***Routine2Date判断***/
var RoutineLastDate = "2018-12-31" //CheckOagRoutine调用的最后日期
func CheckOagRoutine(Routine, Date string) bool {
	if Date <= RoutineLastDate {
		if _, ok := cachestation.Routine2Date[Routine+Date]; ok {
			return true
		} else {
			return false
		}
	}
	return true
}

/***获取智能接驳地(按出发地可以到达的目的地,目的地的热度进行排列的)***/

var Connect_1 int = 50 //用于1次接驳获取的最短航线的中转地数量
var Connect_2 int = 7  //用于2次接驳获取的智能接驳地数量

//目前只用于测试航线路线
func findFlightRoutine(DepartStation, ArriveStation string, nowLeg, maxLeg int, Routine string, Mile int, maxMile int, List map[string]bool) {
	var ListDepart []*cachestation.ListStation
	var ok bool
	nextLeg := nowLeg + 1

	if ListDepart, ok = cachestation.FlightRoutine[DepartStation]; !ok {
		return
	}

	for _, v := range ListDepart {
		mile := Mile + v.Mile

		if mile > maxMile && nowLeg > 0 {
			continue //有的Mile错误,但直飞是必须有效的.
		}

		if v.Station == ArriveStation {
			if nextLeg == maxLeg {
				List[Routine+"-"+v.Station] = true
				return
			} else {
				continue
			}

		} else {
			if nextLeg < maxLeg && //这里放在findFlightRoutine外,比进入函数才判断,极大的提高了性能.
				mile < maxMile && !strings.Contains(Routine, v.Station) {
				findFlightRoutine(v.Station, ArriveStation, nextLeg, maxLeg, Routine+"-"+v.Station, mile, maxMile, List)
			}
		}
	}

	return
}

//findFlightRoutine优化输出,专用与2次接驳
func findFlightRoutine_V2(DepartStation, ArriveStation string, nowLeg, maxLeg int, Routine string, Mile int, maxMile int, aport map[string]int) {
	var ListDepart []*cachestation.ListStation
	var ok bool
	nextLeg := nowLeg + 1

	if ListDepart, ok = cachestation.FlightRoutine[DepartStation]; !ok {
		return
	}

	for _, v := range ListDepart {
		//mile 实质等于传过来的距离再加上现在找到的中转地的距离
		mile := Mile + v.Mile

		if mile > maxMile && nowLeg > 0 {
			continue //有的Mile错误,但直飞是必须有效的.
		}

		//这里是
		if v.Station == ArriveStation {
			if nextLeg == maxLeg {
				aport[Routine[4:7]]++
				return
			} else {
				continue
			}
		} else {
			if nextLeg < maxLeg && //这里放在findFlightRoutine外,比进入函数才判断,极大的提高了性能.
				mile < maxMile && !strings.Contains(Routine, v.Station) {
				findFlightRoutine_V2(v.Station, ArriveStation, nextLeg, maxLeg, Routine+"-"+v.Station, mile, maxMile, aport)
			}
		}
	}

	return
}

//专用于1次转机
func findFlightRoutine_V3(DepartStation, ArriveStation string, nowLeg, maxLeg int, Routine string, Mile int, maxMile int, aport map[string]int) {

	var ListDepart []*cachestation.ListStation
	var ok bool
	nextLeg := nowLeg + 1

	if ListDepart, ok = cachestation.FlightRoutine[DepartStation]; !ok {
		return
	}

	//这里的v代表了出发地能够直飞的那个地方，里面有Mile为距离，Station为对应的那个目的地
	for _, v := range ListDepart {
		mile := Mile + v.Mile

		if mile > maxMile && nowLeg > 0 {
			continue //有的Mile错误,但直飞是必须有效的.
		}

		//如果可以飞达的那个地方，刚好和我们传入的目的地一样
		if v.Station == ArriveStation {

			//航段数和最大的段数一样
			if nextLeg == maxLeg {
				aport[Routine[4:7]] = mile
				return
			} else {
				continue
			}

		} else {
			if nextLeg < maxLeg && //这里放在findFlightRoutine外,比进入函数才判断,极大的提高了性能.
				mile < maxMile && !strings.Contains(Routine, v.Station) {
				findFlightRoutine_V2(v.Station, ArriveStation, nextLeg, maxLeg, Routine+"-"+v.Station, mile, maxMile, aport)
			}
		}
	}

	return
}

//(已过期)获取出发地飞出的目的地的最大N热度中转地
func IntelligentConnect(DepartStation, ArriveStation string, TM int) []string {

	tm := TM

	if TM < 10 {
		tm = 1000
	} else if TM < 200 {
		tm = TM * 4
	} else if TM < 300 {
		tm = TM * 3
	} else if TM < 500 {
		tm = TM * 15 / 10
	}

	if routine, ok := cachestation.FlightRoutine[DepartStation]; ok {
		aport := make(map[string]int, len(routine))

		for _, airport := range routine {

			if airport.Mile > tm || airport.Station == ArriveStation {
				continue
			}

			if subrout, ok := cachestation.FlightRoutine[airport.Station]; ok {
				aport[airport.Station] = len(subrout)
			}
		}

		if len(aport) <= Connect_2 {
			list := make([]string, 0, len(aport))

			for k := range aport {
				list = append(list, k)
			}
			return list

		} else {
			i := Connect_2
			list := make([]string, 0, i)
			for ; i > 0; i-- {
				station := ""
				mun := 0
				for k, v := range aport {
					if v > mun {
						station = k
						mun = v
					}
				}

				list = append(list, station)
				delete(aport, station)
			}

			return list
		}
	}

	return []string{}
}

/***获取智能接驳地(2次中转使用,按出发地可以到达的相同国家的目的地,目的地的热度进行排列的)***/
func IntelligentConnect_V3(DepartStation string) []string {

	var (
		departCountry string
	)

	if county, ok := cachestation.County[DepartStation]; ok {
		departCountry = cachestation.CityCountry[county.City]
	} else {
		return []string{}
	}

	if routine, ok := cachestation.FlightRoutine[DepartStation]; ok {
		aport := make(map[string]int, len(routine))

		for _, airport := range routine {
			if county, ok := cachestation.County[airport.Station]; ok {
				if cachestation.CityCountry[county.City] == departCountry {
					if subrout, ok := cachestation.FlightRoutine[airport.Station]; ok {
						aport[airport.Station] = len(subrout)
					}
				}
			}
		}

		if len(aport) <= Connect_2 {
			list := make([]string, 0, len(aport))

			for k := range aport {
				list = append(list, k)
			}
			return list

		} else {
			i := Connect_2
			list := make([]string, 0, i)
			for ; i > 0; i-- {
				station := ""
				mun := 0
				for k, v := range aport {
					if v > mun {
						station = k
						mun = v
					}
				}

				list = append(list, station)
				delete(aport, station)
			}

			return list
		}
	} else {
		return []string{}
	}
}

/***获取智能接驳地(IntelligentConnect_V2优化性能接口,用于2次接驳调用的中转接驳,也就是LegMode的中转地)***/
func IntelligentConnect_V4(DepartStation, ArriveStation string, maxLeg int, DeviationRate int) ([]string, int) {

	var ListDepart []*cachestation.ListStation
	var ok bool
	if ListDepart, ok = cachestation.FlightRoutine[DepartStation]; !ok {
		return []string{}, 0
	}
	aport := make(map[string]int, len(ListDepart))
	TM := cacheflight.DestGetDistance(DepartStation, ArriveStation)
	tm := TM

	if TM < 10 {
		tm = 1000
	} else if TM < 200 {
		tm = TM * 5
	} else if TM < 300 {
		tm = TM * 4
	} else if TM < 400 {
		tm = TM * 3
	} else {
		if DeviationRate > 100 {
			tm = TM * DeviationRate / 100
		} else {
			tm = TM * 210 / 100
		}
	}

	findFlightRoutine_V2(DepartStation, ArriveStation, 0, maxLeg, DepartStation, 0, tm, aport)

	//fmt.Println("TM=", TM, "tm=", tm, "len(aport)=", len(aport)) ////////

	lenaport := len(aport)

	if lenaport <= Connect_2 {
		list := make([]string, 0, lenaport)

		for k := range aport {
			list = append(list, k)
		}

		return list, lenaport

	} else {
		i := Connect_2
		list := make([]string, 0, i)
		for ; i > 0; i-- {
			station := ""
			mun := 0
			for k, v := range aport {
				if v > mun {
					station = k
					mun = v
				}
			}

			list = append(list, station)
			delete(aport, station)
		}

		return list, Connect_2
	}
}

/***获取智能接驳地(专用于中转1次接驳)***/
func IntelligentConnect_V5(DepartStation, ArriveStation string, maxLeg int, DeviationRate int) ([]string, int) {

	var ListDepart []*cachestation.ListStation
	var ok bool

	//FlightRoutine 主要是用来查询直飞路线的。以出发地为key，查询以这个出发地为出发的key有多少目的地是直飞的
	if ListDepart, ok = cachestation.FlightRoutine[DepartStation]; !ok {
		return []string{}, 0
	}

	aport := make(map[string]int, len(ListDepart))

	//两地的距离
	TM := cacheflight.DestGetDistance(DepartStation, ArriveStation)
	tm := TM

	//这个规则是怎样来的？为什么两地
	if TM < 1000 {
		tm = 5000
	} else if TM < 5000 {
		tm = 15000
	} else {
		tm = TM * DeviationRate / 100
		if tm < 20000 {
			tm = 20000
		}
	}

	findFlightRoutine_V3(DepartStation, ArriveStation, 0, maxLeg, DepartStation, 0, tm, aport)

	//fmt.Println("TM=", TM, "tm=", tm, "len(aport)=", len(aport)) ////////

	lenaport := len(aport)

	if lenaport <= Connect_1 {
		list := make([]string, 0, lenaport)

		for k := range aport {
			list = append(list, k)
		}

		return list, lenaport

	} else {
		i := Connect_1
		list := make([]string, 0, i)
		for ; i > 0; i-- {
			station := ""
			mun := 100000
			for k, v := range aport {
				if v < mun {
					station = k
					mun = v
				}
			}

			list = append(list, station)
			delete(aport, station)
		}

		return list, lenaport
	}
}

/***查询是否有对应的航线票单***/
func RoutineCheckSelect(rout []*mysqlop.Routine) (Count int) {
	for _, leg := range rout {
		for _, departcounty := range leg.DepartCounty {
			for _, arrivecounty := range leg.ArriveCounty {
				Count += mysqlop.RoutineCheckSelect(departcounty, arrivecounty, leg.TravelDate)
			}
		}
	}

	return
}

//写日志函数
func writeLog(dc *DebugControl, DealID int, rmb mysqlop.ResultMainBill, qfi_fore, qfi_fore_web []cacheflight.FlightJSON) {
	if dc == nil {
		return
	}
	rb, _ := json.Marshal(rmb)
	fareStr := string(rb)
	rb, _ = json.Marshal(qfi_fore)
	flightStr := string(rb)
	var rb2 []byte
	if qfi_fore_web != nil {
		rb2, _ = json.Marshal(qfi_fore_web)
	}
	dc.Open()
	dc.AddInfo("", "机场分支处理: "+strconv.Itoa(DealID))
	dc.AddInfo("本地缓存Fare信息", fareStr)
	dc.AddInfo("本地缓存Flight信息", flightStr)
	if qfi_fore_web != nil {
		dc.AddInfo("远程获取Flight信息", string(rb2))
	}
	dc.Close()
}

//外来供应商获取缓存

//#TODO 在这里回调ks_keycache----keychche---isp----mh 等各供应商数据...返回的数据就是（Fare和票单）
func getBlockCache(agency string, depart, arrive []string, traveldate, backdate string, quick bool) (*cacheflight.BlockCache, error) {

	//在这里加多一个参数进去。如果加舱位进去的话看是否有问题
	data, _ := json.Marshal(&struct {
		Agency     string   `json:"Agency"`
		Depart     []string `json:"Depart"`
		Arrive     []string `json:"Arrive"`
		TravelDate string   `json:"TravelDate"`
		BackDate   string   `json:"BackDate"`
		Quick      bool     `json:"Quick"`
	}{
		Agency:     agency,
		Depart:     depart,
		Arrive:     arrive,
		TravelDate: traveldate,
		BackDate:   backdate,
		Quick:      quick,
	})

	body := bytes.NewBuffer(data)
	//#TODO   这个地方开始调用ks——keycache里面的接口，进行shopping。   之后还将结果缓存起来。  这里其实就是进行一次中转。利用上个接口的请求，传递到下一饿接口进去
	s := "http://" + keycacheIP + ":8888/WAPI_KeyCache"
	res, err := http.Post(s, "application/json;charset=utf-8", body)
	if err != nil {
		return nil, err
	}

	result, err := ioutil.ReadAll(res.Body)
	res.Body.Close()
	if err != nil {
		return nil, err
	}

	gnr, err := gzip.NewReader(bytes.NewBuffer(result))
	if err != nil {
		return nil, err
	}
	defer gnr.Close()
	result, _ = ioutil.ReadAll(gnr)
	if len(result) < 100 {
		return nil, errors.New(string(result))
	}

	//在这里拿到对应的数据。接着返回出去
	var bc cacheflight.BlockCache
	if err := json.Unmarshal(result, &bc); err != nil {
		return nil, err
	}

	//#TODO 这里来过滤舱位；如果是Y舱的话，则可以去ES里面拿；如果不是Y舱，则跳过ES

	//bc.Fares.Mainbill[0].Agency

	return &bc, nil
}

//其实getBlockCache_V2 是getBlockCache的升级版。只不过是将返回的数据，利用chan的方式，类似回调，放到参数里面去而已。本质上都还是去调用getBlockCache
func getBlockCache_V2(agency string, depart, arrive []string, traveldate, backdate string, quick bool, bcc chan *cacheflight.BlockCache) {
	var bc *cacheflight.BlockCache

	defer func() {
		bcc <- bc
	}()

	bc, _ = getBlockCache(agency, depart, arrive, traveldate, backdate, quick)
}

//获取有效的供应商Routine
func useRoutine(routine []*cachestation.AgRoutine, rout *mysqlop.Routine) []*cachestation.AgRoutine {
	if len(routine) == 0 {
		return nil
	}

	sourceRoutine := make([]*cachestation.AgRoutine, 0, len(routine))
	departs := strings.Join(rout.DepartCounty, " ")
	arrives := strings.Join(rout.ArriveCounty, " ")

	for _, rout := range routine {
		if len(rout.Routine) == 17 {
			if strings.Contains(departs, rout.Routine[:3]) &&
				strings.Contains(arrives, rout.Routine[14:]) {
				sourceRoutine = append(sourceRoutine, rout)
			}
		} else if len(rout.Routine) == 24 {
			if strings.Contains(departs, rout.Routine[:3]) &&
				strings.Contains(arrives, rout.Routine[21:]) {
				sourceRoutine = append(sourceRoutine, rout)
			}
		}
	}
	return sourceRoutine
}

//分开供应商表
func splitUseRoutine(useRoutine []*cachestation.AgRoutine) (map[string]struct{}, map[string]struct{}) {
	use1 := map[string]struct{}{}
	use2 := map[string]struct{}{}
	for _, uk := range useRoutine {
		k := strings.Split(uk.Routine, "-")
		if len(k) == 5 {
			use1[strings.Join(k[:3], "-")] = struct{}{}
			use2[strings.Join(k[2:], "-")] = struct{}{}
		} else if len(k) == 7 {
			if uk.PS { //前续
				use1[strings.Join(k[:3], "-")] = struct{}{}
				use2[strings.Join(k[2:], "-")] = struct{}{}
			} else { //后续
				use1[strings.Join(k[:5], "-")] = struct{}{}
				use2[strings.Join(k[4:], "-")] = struct{}{}
			}
		}
	}

	return use1, use2
}

//把供应商配置表中的路线拆分
func getQueryRoutine(useRoutine map[string]struct{}) ([][2][]string, []string, []string) { //多次查询中的出发地组和目的地组
	notin := func(ss []string, v string) bool {
		i := 0
		for ; i < len(ss); i++ {
			if ss[i] == v {
				break
			}
		}
		return i == len(ss)
	}

	mr := make(map[string][]string, len(useRoutine))
	for uk := range useRoutine {
		k := strings.Split(uk, "-")
		v := mr[k[0]]
		switch len(k) {
		case 3:
			if v == nil || notin(v, k[2]) {
				mr[k[0]] = append(v, k[2])
			}
		case 5:
			if v == nil || notin(v, k[4]) {
				mr[k[0]] = append(v, k[4])
			}
		}
	}

	mret := make(map[string][]string, len(mr))
	for k, v := range mr {

		//#TODO 20181102 由于需要在linux上面编译，升哥建议将sort.Slice 注释
		//sort.Slice(v, func(i, j int) bool { return v[i] <= v[j] })
		kk := strings.Join(v, " ")
		kv := mret[kk]
		if kv == nil || notin(kv, k) {
			mret[kk] = append(mret[kk], k)
		}
	}

	ret := make([][2][]string, 0, len(mret))

	for k, v := range mret {
		//#TODO 20181102 由于需要在linux上面编译，升哥建议将sort.Slice 注释
//		sort.Slice(v, func(i, j int) bool { return v[i] <= v[j] })
		ret = append(ret, [2][]string{v, strings.Split(k, " ")})
	}


	var dc, ac []string
	for _, qr := range ret {
		for dci := range qr[0] {
			if notin(dc, qr[0][dci]) {
				dc = append(dc, qr[0][dci])
			}
		}
		for aci := range qr[1] {
			if notin(ac, qr[1][aci]) {
				ac = append(ac, qr[1][aci])
			}
		}
	}

	return ret, dc, ac
}

//获取Office适合的数据源供应商
func getProvider(Offices []string) map[string]struct{} {

	IAs := make(map[string]struct{}, len(cachestation.AgencyFrame)+len(cachestation.AgencyGroup))
	for _, agent := range cachestation.AgencyFrame {
		IAs[agent.ShowShoppingName] = struct{}{}
	}
	for _, agent := range cachestation.AgencyGroup {
		IAs[agent.ShowShoppingName] = struct{}{}
	}

	mds := make(map[string]struct{}, len(cachestation.AgencyFrame)+len(cachestation.AgencyGroup))
	for _, OfficeID := range Offices {
		if model_2s, ok := cachestation.Model_3[OfficeID]; ok {
			for _, m2 := range model_2s {
				if _, ok2 := IAs[m2.DataSource]; ok2 {
					mds[m2.DataSource] = struct{}{}
				}
			}
		}
	}
	//mds["FBFB"] = struct{}{} //测试使用
	//mds["W5"] = struct{}{}   //测试使用

	return mds
}

//获取供应商的信息
func getAllAgencis() []cachestation.IAgency {
	IAs := make([]cachestation.IAgency, 0, len(cachestation.AgencyFrame)+len(cachestation.AgencyGroup))
	for _, agent := range cachestation.AgencyFrame {
		IAs = append(IAs, agent)
	}

	for _, agent := range cachestation.AgencyGroup {
		IAs = append(IAs, agent)
	}
	return IAs
}
