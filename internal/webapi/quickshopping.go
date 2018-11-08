package webapi

import (
	"bytes"
	"cacheflight"
	"cachestation"
	"compress/gzip"
	"encoding/json"
	"errorlog"
	"fmt"
	"io/ioutil"
	"mysqlop"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

var fareServerIP = "10.205.4.114"
var keycacheIP = "10.205.4.114"

type Conns struct {
	DepartStation string   `json:"DS"`
	ArriveStation string   `json:"AS"`
	ConnStation   []string `json:"CS"`
}

type QCS struct {
	ConnStations []*Conns `json:"Conns"`
	Havefare     bool     `json:"HF"`
}

type ListPoint2Point_Output struct {
	ListShopping []*Point2Point_Output `json:"ListShopping"`
}

var nullOutput = &Point2Point_Output{

	Result:  2,
	Segment: []*Point2Point_RoutineInfo2Segment{},
	Fare:    []*FareInfo{},
	Journey: []*JourneyInfo{}}

//获取HotJourney中的每个航司的最小价格票单
func (this *ListPoint2Point_Output) Lower() {
	sc := make(map[string]*Point2Point_RoutineInfo2Segment, len(this.ListShopping)*20)
	fc := make(map[string]*FareInfo, len(this.ListShopping)*20)
	outJourAirline := make(map[string]struct{}, len(this.ListShopping)*20) //string = Index + Airline

	for index, output := range this.ListShopping {
		if len(output.Journey) <= 1 { //HotJourney中只返回非nil的Shopping
			continue
		}

		for _, ri := range output.Segment {
			sc[ri.SC] = ri //这里是不会重复的,因为存在DealID
		}

		for _, fi := range output.Fare {
			fc[fi.FareCode] = fi //这里重复的fare.ID是不怕的,因Fare本来就被多个Shopping多次使用
		}

		journeys := output.Journey[:0]
		tmp_sc := make(map[string]*Point2Point_RoutineInfo2Segment, len(output.Segment))
		tmp_fc := make(map[string]*FareInfo, len(output.Fare))

		for _, outJour := range output.Journey {

			//如果存在2单组成的双程或称2Shopping组成的Shopping这里是存在缺陷的
			if _, ok := outJourAirline[strconv.Itoa(index)+outJour.JourneyTicket[0].MarketingAirline]; !ok {

				outJourAirline[strconv.Itoa(index)+outJour.JourneyTicket[0].MarketingAirline] = struct{}{}
				journeys = append(journeys, outJour)

				for _, js := range outJour.JourneySegment {
					tmp_sc[js.SC] = sc[js.SC]
					tmp_fc[js.FC] = fc[js.FC]
				}
			}
		}

		output.Journey = journeys
		output.Segment = output.Segment[:0]
		output.Fare = output.Fare[:0]

		for _, segment := range tmp_sc {
			output.Segment = append(output.Segment, segment)
		}

		for _, fare := range tmp_fc {
			output.Fare = append(output.Fare, fare)
		}

	}
}

func RoutineCheckSelect_V2(b []byte) *QCS {

	body := bytes.NewBuffer([]byte(b))

	s := "http://" + fareServerIP + ":9999/WAPI_RoutineCheckSelect"
	res, err := http.Post(s, "application/json;charset=utf-8", body)
	if err != nil {
		return &QCS{ConnStations: []*Conns{}}
	}
	defer res.Body.Close()

	result, err := ioutil.ReadAll(res.Body)

	if err != nil {
		return &QCS{ConnStations: []*Conns{}}
	}

	var qcs QCS

	if err := json.Unmarshal([]byte(result), &qcs); err != nil {
		return &QCS{ConnStations: []*Conns{}}
	}
	return &qcs
}

func RoutineCheckSelect_V3(p2pi *Point2Point_In, chQcs chan *QCS) {
	b, _ := json.Marshal(p2pi)
	chQcs <- RoutineCheckSelect_V2(b)
}

//查询票单。
func QueryMainBill_V2(
	p2pi *Point2Point_In,
	rout []*mysqlop.Routine,
	TheTrip bool,
	days int,
	crmb chan *mysqlop.MutilDaysMainBill) {

	p2p_in := *p2pi //将传过来的值重新copy一个

	p2p_in.Deal = "QueryDepartDays"
	p2p_in.Days = days
	p2p_in.TheTrip = TheTrip //控制是否只获取双程  //单双程

	//rout大于0，则直接将rout赋值。空的话，则直接赋值
	if len(rout) > 0 {
		p2p_in.Rout = rout
		if len(rout[0].DepartCounty) > len(rout[0].ArriveCounty) {
			p2p_in.Deal = "QueryArriveDays"
		}
	} else {
		p2p_in.Rout = []*mysqlop.Routine{}
	}

	b, err := json.Marshal(&p2p_in)
	if err != nil {
		crmb <- &mysqlop.MutilDaysMainBill{Fares: [][2]mysqlop.ListMainBill{}}
		return
	}
	body := bytes.NewBuffer([]byte(b))

	//WAPI_QueryFare    终于出现了。。。。。。 quickshopping和分布式服务开始挂钩了
	s := "http://" + fareServerIP + ":9999/WAPI_QueryFare"
	res, err := http.Post(s, "application/json;charset=utf-8", body)
	if err != nil {
		crmb <- &mysqlop.MutilDaysMainBill{Fares: [][2]mysqlop.ListMainBill{}}
		return
	}
	defer res.Body.Close()

	if gnr, err := gzip.NewReader(res.Body); err == nil {
		defer gnr.Close()

		result, err := ioutil.ReadAll(gnr)

		if err != nil {
			crmb <- &mysqlop.MutilDaysMainBill{Fares: [][2]mysqlop.ListMainBill{}}
			return
		}

		var rmb mysqlop.MutilDaysMainBill

		if err := json.Unmarshal([]byte(result), &rmb); err != nil {
			crmb <- &mysqlop.MutilDaysMainBill{Fares: [][2]mysqlop.ListMainBill{}}
			return
		}

		//不指定BCC,必须过滤;指定时出相关BCC.
		camap := make(map[string][]string, 30)
		gdspcc := make(map[string]struct{}, 10)
		for _, OfficeID := range p2pi.Offices {
			if model_2s, ok := cachestation.Model_3[OfficeID]; ok {
				for _, m2 := range model_2s {
					cachestation.DC_Airline_BCC.Mutex.RLock()
					airline_bcc, ok2 := cachestation.DC_Airline_BCC.Airline_BCC[m2.DiscountCode]
					cachestation.DC_Airline_BCC.Mutex.RUnlock()

					if ok2 {
						for airline, bcc := range airline_bcc {
							camap[airline] = append(camap[airline], bcc)
						}
					}

					//过滤不适合的GDS,PCC票单记录
					gdspcc[m2.DataSource+"/"+m2.PCC] = struct{}{}
				}
			}
		}

		//下面2行是测试的,记得删除
		//gdspcc["1G/63BS"] = struct{}{}
		//gdspcc["1G/7K1I"] = struct{}{}

		cmap := make(map[string]string, len(camap))
		for airline, bcc := range camap {
			cmap[airline] = strings.Join(bcc, "/")
		}

		var ok1, ok2 bool
		for m, v1 := range rmb.Fares { //Day
			for n, v2 := range v1 { //Go,Back
				mbl := len(v2)
				for i := 0; i < mbl; {
					//fmt.Println(mbl, "M =", v2[i].BillID, v2[i].Routine, v2[i].Berth, v2[i].ApplyAir, v2[i].Agency, v2[i].PCC) /////
					_, ok1 = gdspcc[v2[i].Agency+"/"+v2[i].PCC]
					if v2[i].Agency != "1E" { //这里是FB
						_, ok2 = gdspcc[v2[i].Agency+"/"]
					} else { //GDS="1E"时,PCC默认为"CAN131"
						ok2 = false
					}

					if !ok1 && !ok2 || //GDS,PCC不适合
						//!strings.HasPrefix(v2[i].BillID, prefix_FareV2) ||
						v2[i].BigCustomerCode != "" &&
							!strings.Contains(cmap[v2[i].AirInc], v2[i].BigCustomerCode) {

						mbl--
						v2[i], v2[mbl] = v2[mbl], v2[i]

					} else {
						//fmt.Println("MB=", v2[i].BillID, v2[i].Routine, v2[i].Berth, v2[i].ApplyAir, v2[i].Agency, v2[i].PCC) /////
						if v2[i].MixBerth == 0 {
							v2[i].MixBerth = 2 //允许混舱
						}
						i++
					}
				}
				if len(v2) != mbl {
					rmb.Fares[m][n] = v2[:mbl] //因为[2]ListMainBill时一个值,所以修改里面必须指定到具体
				}
			}
		}
		crmb <- &rmb
	} else {
		crmb <- &mysqlop.MutilDaysMainBill{Fares: [][2]mysqlop.ListMainBill{}}
	}
}

//Fare,FlightTime都是远程缓存的查询/多天(Rout==[Go/Back])
func QueryFareAndFlighttime(
	addDays int, //组合供应商必须冗余的天数
	p2pi *Point2Point_In, //查询条件
	rout []*mysqlop.Routine) ( //rout是真实取获取的行段
	*mysqlop.MutilDaysMainBill, //[Days(Go/Back)]
	[][]*cacheflight.FlightJSON) { //[Go/Back][Days]

	var (
		//Fare
		crmb chan *mysqlop.MutilDaysMainBill //票单静态数据
		//fareTimes *mysqlop.MutilDaysMainBill      //查询的航班结果集

		//FlightTime
		chanFT      [][]chan *cacheflight.FlightJSON //[rout][county]
		flightTimes [][]*cacheflight.FlightJSON      //[rout][days]

		//other
		routLen        = len(rout)
		daysFlightTime []map[string]*cacheflight.FlightJSON //[Go/Back][days]
	)

	crmb = make(chan *mysqlop.MutilDaysMainBill, 1)
	go QueryMainBill_V2(p2pi, rout, false, p2pi.Days+addDays, crmb)

	chanFT = make([][]chan *cacheflight.FlightJSON, routLen)
	flightTimes = make([][]*cacheflight.FlightJSON, routLen)
	daysFlightTime = make([]map[string]*cacheflight.FlightJSON, routLen)

	for i, leg := range rout {
		daysFlightTime[i] = make(map[string]*cacheflight.FlightJSON, p2pi.Days+addDays)
		flightTimes[i] = make([]*cacheflight.FlightJSON, p2pi.Days+addDays)

		if len(leg.DepartCounty) < len(leg.ArriveCounty) { //这里不是<=的关键是因为Back的数据少,而且操作少一步.
			j := len(leg.DepartCounty)
			chanFT[i] = make([]chan *cacheflight.FlightJSON, j, j)

			for j, departcounty := range leg.DepartCounty {
				chanFT[i][j] = make(chan *cacheflight.FlightJSON, 1)
				go QueryLegInfo_V2(&cacheflight.RoutineService{
					Deal:           "QueryNearForeDays",
					DepartStation:  departcounty,
					ConnectStation: leg.ArriveCounty,
					ArriveStation:  "***",
					TravelDate:     leg.TravelDate,
					Days:           p2pi.Days + addDays,
					Quick:          p2pi.Quick},
					cachestation.PetchIP[departcounty], chanFT[i][j])
			}
		} else {
			j := len(leg.ArriveCounty)
			chanFT[i] = make([]chan *cacheflight.FlightJSON, j, j)

			for j, arrivecounty := range leg.ArriveCounty {
				chanFT[i][j] = make(chan *cacheflight.FlightJSON, 1)
				go QueryLegInfo_V2(&cacheflight.RoutineService{
					Deal:           "QueryNearBackDays",
					DepartStation:  "***",
					ConnectStation: leg.DepartCounty,
					ArriveStation:  arrivecounty,
					TravelDate:     leg.TravelDate,
					Days:           p2pi.Days + addDays,
					Quick:          p2pi.Quick},
					cachestation.PetchIP[arrivecounty], chanFT[i][j])
			}
		}
	}

	days := make([]time.Time, 0, routLen)
	for _, leg := range rout {
		day, _ := time.Parse("2006-01-02", leg.TravelDate)
		days = append(days, day)
	}

	fareCut2Days := func(Segment int, flightTime *cacheflight.FlightJSON) {
		for _, ri := range flightTime.Route {
			if fjson, ok := daysFlightTime[Segment][ri.FI[0].Legs[0].DSD]; ok { //Segment = [Go/Back]
				fjson.Route = append(fjson.Route, ri)
			} else {
				daysFlightTime[Segment][ri.FI[0].Legs[0].DSD] = &cacheflight.FlightJSON{
					Route: append(make([]*cacheflight.RoutineInfoStruct, 0, 15), ri)}
			}
		}
	}

	for i := range chanFT {
		for j := range chanFT[i] {
			fareCut2Days(i, <-chanFT[i][j])
		}
	}

	for Segment := 0; Segment < routLen; Segment++ {
		for day := 0; day < p2pi.Days+addDays; day++ {
			traveldate := days[Segment].AddDate(0, 0, day).Format("2006-01-02")
			if flightTime, ok := daysFlightTime[Segment][traveldate]; ok {
				flightTimes[Segment][day] = flightTime
			} else {
				flightTimes[Segment][day] = &cacheflight.FlightJSON{}
			}
		}
	}

	return <-crmb, flightTimes
}

//封装QueryFareAndFlighttime输出为chan
func QueryFareAndFlighttime_V2(
	addDays int, //组合供应商必须冗余的天数
	p2pi *Point2Point_In, //查询条件
	rout []*mysqlop.Routine, //rout是真实取获取的行段
	rmbChan chan *mysqlop.MutilDaysMainBill, //[Days(Go/Back)]
	flightChan chan [][]*cacheflight.FlightJSON) { //[Go/Back][Days]

	defer errorlog.DealRecoverLog()
	var fares *mysqlop.MutilDaysMainBill
	var flightTimes [][]*cacheflight.FlightJSON

	defer func() {
		rmbChan <- fares
		flightChan <- flightTimes
	}()

	fares, flightTimes = QueryFareAndFlighttime(addDays, p2pi, rout)
}

//Fare,FlightTime都是远程缓存的查询/多天
func QueryFareAndFlighttimeWithConns(
	p2pi *Point2Point_In, //查询条件
	rout []*mysqlop.Routine,
	qcs *QCS,
	addLegOne int, //第一段添加的额外天数
	addLegTwo int) ( //第二段添加的额外天数
	[][2]*mysqlop.MutilDaysMainBill, //[Routine][LegOne(Go&Back)/LegTwo(Go&Back)][Days(Go/Back)]
	[][][]*cacheflight.FlightJSON) { //[Routine(按qcs次序)][GoLeg1/GoLeg2/BackLeg1/BackLeg2][days]

	var (
		//Fare
		newRoutLegOne, newRoutLegTwo []*mysqlop.Routine
		fares                        [][2]*mysqlop.MutilDaysMainBill      //[Routine][LegOne/LegTwo][Days]
		chanFares                    [][2]chan *mysqlop.MutilDaysMainBill //[Routine][LegOne/LegTwo]

		//FlightTime
		flightTimes [][][]*cacheflight.FlightJSON    //[Routine][GoLeg1/GoLeg2/BackLeg1/BackLeg2][days]
		chanFT      [][]chan *cacheflight.FlightJSON //[Routine][GoLeg1/GoLeg2/BackLeg1/BackLeg2]

		//other
		qcsLen  int = len(qcs.ConnStations)
		routLen int = len(rout)

		daysFlightTime [][]map[string]*cacheflight.FlightJSON //[Routine][GoLeg1/GoLeg2/BackLeg1/BackLeg2][day]
	)

	fares = make([][2]*mysqlop.MutilDaysMainBill, qcsLen)
	chanFares = make([][2]chan *mysqlop.MutilDaysMainBill, qcsLen)
	flightTimes = make([][][]*cacheflight.FlightJSON, qcsLen)
	chanFT = make([][]chan *cacheflight.FlightJSON, qcsLen)
	daysFlightTime = make([][]map[string]*cacheflight.FlightJSON, qcsLen)

	for i, leg := range qcs.ConnStations {
		//跟随初始化
		flightTimes[i] = make([][]*cacheflight.FlightJSON, routLen*2)
		chanFT[i] = make([]chan *cacheflight.FlightJSON, routLen*2)
		daysFlightTime[i] = make([]map[string]*cacheflight.FlightJSON, routLen*2)
		for j := 0; j < routLen*2; j++ { //GoLeg1/GoLog2/BackLeg1/BackLeg2
			flightTimes[i][j] = make([]*cacheflight.FlightJSON, p2pi.Days+addLegTwo)
			daysFlightTime[i][j] = make(map[string]*cacheflight.FlightJSON, p2pi.Days+addLegTwo)
		}

		//流程处理开始
		DepartStation := leg.DepartStation //Airport
		ArriveStation := leg.ArriveStation //Airport
		ConnectStations := leg.ConnStation //Airport

		//下面是Fare阶段
		newRoutLegOne = make([]*mysqlop.Routine, 0, routLen)
		newRoutLegTwo = make([]*mysqlop.Routine, 0, routLen)

		newRoutLegOne = append(newRoutLegOne, &mysqlop.Routine{
			DepartStation: rout[0].DepartStation,
			DepartCounty:  []string{DepartStation},
			ArriveStation: rout[0].ArriveStation,
			ArriveCounty:  ConnectStations,
			TravelDate:    rout[0].TravelDate,
			Wkday:         rout[0].Wkday,
			DayDiff:       rout[0].DayDiff,
			Segment:       rout[0].Segment,
			Trip:          rout[0].Trip,
			GoorBack:      rout[0].GoorBack,
			CrossSeason:   rout[0].CrossSeason,
			Stay:          rout[0].Stay})

		newRoutLegTwo = append(newRoutLegTwo, &mysqlop.Routine{
			DepartStation: rout[0].DepartStation,
			DepartCounty:  ConnectStations,
			ArriveStation: rout[0].ArriveStation,
			ArriveCounty:  []string{ArriveStation},
			TravelDate:    rout[0].TravelDate,
			Wkday:         rout[0].Wkday,
			DayDiff:       rout[0].DayDiff,
			Segment:       rout[0].Segment,
			Trip:          rout[0].Trip,
			GoorBack:      rout[0].GoorBack,
			CrossSeason:   rout[0].CrossSeason,
			Stay:          rout[0].Stay})

		if routLen == 2 {
			newRoutLegOne = append(newRoutLegOne, &mysqlop.Routine{
				DepartStation: rout[1].DepartStation,
				DepartCounty:  ConnectStations,
				ArriveStation: rout[1].ArriveStation,
				ArriveCounty:  []string{DepartStation},
				TravelDate:    rout[1].TravelDate,
				Wkday:         rout[1].Wkday,
				DayDiff:       rout[1].DayDiff,
				Segment:       rout[1].Segment,
				Trip:          rout[1].Trip,
				GoorBack:      rout[1].GoorBack,
				CrossSeason:   rout[1].CrossSeason,
				Stay:          rout[1].Stay})

			newRoutLegTwo = append(newRoutLegTwo, &mysqlop.Routine{
				DepartStation: rout[1].DepartStation,
				DepartCounty:  []string{ArriveStation},
				ArriveStation: rout[1].ArriveStation,
				ArriveCounty:  ConnectStations,
				TravelDate:    rout[1].TravelDate,
				Wkday:         rout[1].Wkday,
				DayDiff:       rout[1].DayDiff,
				Segment:       rout[1].Segment,
				Trip:          rout[1].Trip,
				GoorBack:      rout[1].GoorBack,
				CrossSeason:   rout[1].CrossSeason,
				Stay:          rout[1].Stay})
		}

		chanFares[i][0] = make(chan *mysqlop.MutilDaysMainBill, 1)
		chanFares[i][1] = make(chan *mysqlop.MutilDaysMainBill, 1)
		go QueryMainBill_V2(p2pi, newRoutLegOne, false, p2pi.Days+addLegTwo, chanFares[i][0])
		go QueryMainBill_V2(p2pi, newRoutLegTwo, true, p2pi.Days+addLegTwo, chanFares[i][1])

		//下面是FlilghtTime阶段
		chanFT[i][0] = make(chan *cacheflight.FlightJSON, 1)
		go QueryLegInfo_V2(&cacheflight.RoutineService{
			Deal:           "QueryNearForeDays",
			DepartStation:  DepartStation,
			ConnectStation: ConnectStations,
			ArriveStation:  "***",
			TravelDate:     rout[0].TravelDate,
			Days:           p2pi.Days + addLegOne,
			Quick:          p2pi.Quick},
			cachestation.PetchIP[DepartStation], chanFT[i][0])

		chanFT[i][1] = make(chan *cacheflight.FlightJSON, 1)
		go QueryLegInfo_V2(&cacheflight.RoutineService{
			Deal:           "QueryNearBackDays",
			DepartStation:  "***",
			ConnectStation: ConnectStations,
			ArriveStation:  ArriveStation,
			TravelDate:     rout[0].TravelDate,
			Days:           p2pi.Days + addLegTwo,
			Quick:          p2pi.Quick},
			cachestation.PetchIP[ArriveStation], chanFT[i][1])

		if routLen == 2 {
			chanFT[i][2] = make(chan *cacheflight.FlightJSON, 1)
			go QueryLegInfo_V2(&cacheflight.RoutineService{
				Deal:           "QueryNearForeDays",
				DepartStation:  ArriveStation,
				ConnectStation: ConnectStations,
				ArriveStation:  "***",
				TravelDate:     rout[1].TravelDate,
				Days:           p2pi.Days + addLegOne,
				Quick:          p2pi.Quick},
				cachestation.PetchIP[ArriveStation], chanFT[i][2])

			chanFT[i][3] = make(chan *cacheflight.FlightJSON, 1)
			go QueryLegInfo_V2(&cacheflight.RoutineService{
				Deal:           "QueryNearBackDays",
				DepartStation:  "***",
				ConnectStation: ConnectStations,
				ArriveStation:  DepartStation,
				TravelDate:     rout[1].TravelDate,
				Days:           p2pi.Days + addLegTwo,
				Quick:          p2pi.Quick},
				cachestation.PetchIP[DepartStation], chanFT[i][3])
		}
	}

	days := make([]time.Time, 0, routLen)
	for _, leg := range rout {
		day, _ := time.Parse("2006-01-02", leg.TravelDate)
		days = append(days, day)
	}

	fareCut2Days := func(Routine int, Segment int, flightTime *cacheflight.FlightJSON) { //Segment=GoLeg1/GoLog2/BackLeg1/BackLeg2
		for _, ri := range flightTime.Route {
			if fjson, ok := daysFlightTime[Routine][Segment][ri.FI[0].Legs[0].DSD]; ok {
				fjson.Route = append(fjson.Route, ri)
			} else {
				daysFlightTime[Routine][Segment][ri.FI[0].Legs[0].DSD] = &cacheflight.FlightJSON{
					Route: append(make([]*cacheflight.RoutineInfoStruct, 0, 15), ri)}
			}
		}
	}

	for Routine := 0; Routine < qcsLen; Routine++ {
		fares[Routine][0] = <-chanFares[Routine][0]
		fares[Routine][1] = <-chanFares[Routine][1]

		for Segment := 0; Segment < routLen*2; Segment++ {
			fareCut2Days(Routine, Segment, <-chanFT[Routine][Segment])
		}
	}

	for Routine := 0; Routine < qcsLen; Routine++ {
		for Segment := 0; Segment < routLen*2; Segment++ {
			for day := 0; day < p2pi.Days+addLegTwo; day++ {
				traveldate := days[Segment/2].AddDate(0, 0, day).Format("2006-01-02")
				if flightTime, ok := daysFlightTime[Routine][Segment][traveldate]; ok {
					//sort.Sort(flightTime.Route)
					flightTimes[Routine][Segment][day] = flightTime
				} else {
					flightTimes[Routine][Segment][day] = &cacheflight.FlightJSON{}
				}
			}
		}
	}

	return fares, flightTimes
}

/************快速Shopping多天单出票处理***************/
func QuickOneTicketDeal_V1(
	af *cachestation.Agencies, //供应商
	DaysShoppingChanMB []chan []*MB2RouteInfo, //多天
	DaysShoppingChanOut []chan *Point2Point_Output,
	QuickShopping bool, //是否快速票单
	dc *DebugControl, //写Debug日志
	fareTimes *mysqlop.MutilDaysMainBill, //[Days(Go/Back)]
	flightTimes [][]*cacheflight.FlightJSON, //[Go/Back][Days]
	p2pi *Point2Point_In, //查询条件
	rout []*mysqlop.Routine) {

	routLen := len(rout)
	DealID := 0

	days := make([]time.Time, 0, routLen)

	//days是一个切片数组。将旅行的日期全都拼到days这个数组里面去
	for _, leg := range rout {
		day, _ := time.Parse("2006-01-02", leg.TravelDate)
		days = append(days, day)
	}

	for day := 0; day < p2pi.Days; day++ {
		var NewRout []*mysqlop.Routine
		var Fares mysqlop.ResultMainBill
		var Flights []cacheflight.FlightJSON

		if routLen == 1 {
			R0 := *rout[0]
			R0.TravelDate = days[0].AddDate(0, 0, day).Format("2006-01-02")
			NewRout = []*mysqlop.Routine{&R0}
		} else if routLen == 2 {
			R0 := *rout[0]
			R1 := *rout[1]
			R0.TravelDate = days[0].AddDate(0, 0, day).Format("2006-01-02")
			R1.TravelDate = days[1].AddDate(0, 0, day).Format("2006-01-02")
			NewRout = []*mysqlop.Routine{&R0, &R1}
		}

		if flightTimes != nil {
			if routLen == 1 && len(fareTimes.Fares[day]) == 1 {
				Fares = mysqlop.ResultMainBill{Mainbill: fareTimes.Fares[day][0]}
				Flights = []cacheflight.FlightJSON{*flightTimes[0][day]}
			} else {
				if fareTimes.Fares != nil && len(fareTimes.Fares) > day && len(fareTimes.Fares[day]) == 2 {
					Fares = mysqlop.ResultMainBill{Mainbill: append(fareTimes.Fares[day][0], fareTimes.Fares[day][1]...)}
				} else {
					Fares = mysqlop.ResultMainBill{}
				}

				if flightTimes != nil && len(flightTimes) == 2 &&
					len(flightTimes[0]) > day && len(flightTimes[1]) > day {
					Flights = []cacheflight.FlightJSON{*flightTimes[0][day], *flightTimes[1][day]}
				} else {
					Flights = []cacheflight.FlightJSON{}
				}
			}
		}

		DealID++

		if af.SaveAs == "Cache" {
			go Point2Point_V3(QuickShopping, dc, Fares, Flights, DealID, p2pi, NewRout, false, false, af, DaysShoppingChanMB[day], DaysShoppingChanOut[day])
		} else {
			go Point2Point_V4(QuickShopping, dc, DealID, p2pi, NewRout, false, false, af, DaysShoppingChanMB[day], DaysShoppingChanOut[day])
		}
	}

	return
}

/************快速Shopping多天两张票合并处理************/
//V1版本限制为2张{票单Fares}都只出一张票{Ticket}

func QuickTwoTicketDeal_V1(
	af *cachestation.Agencies,
	QuickShopping bool, //是否快速票单
	dc *DebugControl, //写Debug日志
	p2pi *Point2Point_In, //查询条件
	rout []*mysqlop.Routine,
	qcs *QCS, //远程返回的接驳中转地,而且这里的数据只是去程的接驳
	fareTimes [][2]*mysqlop.MutilDaysMainBill, //HotJourney Param[Routine][LegOne(Go&Back)/LegTwo(Go&Back)][Days(Go/Back)]
	flightTimes [][][]*cacheflight.FlightJSON) ( //HotJourney Param[Routine(按qcs次序)][GoLeg1/GoLeg2/BackLeg1/BackLeg2][days]
	shoppingout *ListPoint2Point_Output) {

	shoppingout = &ListPoint2Point_Output{ListShopping: make([]*Point2Point_Output, 0, p2pi.Days)}

	qcsLen := len(qcs.ConnStations)
	routLen := len(rout)
	if routLen == 2 && rout[0].Trip == 0 { //目前不操作缺口程数据
		return
	}

	if fareTimes == nil || flightTimes == nil {
		fareTimes, flightTimes = QueryFareAndFlighttimeWithConns(p2pi, rout, qcs, 0, 1)
	}
	DealID := 0
	cTicketOut := make([][][4]chan *Point2Point_Output, p2pi.Days)

	//处理日期是因为加价公式的需要.加价的另一条件是否季度未处理
	days := make([]time.Time, 0, routLen)
	for _, leg := range rout {
		day, _ := time.Parse("2006-01-02", leg.TravelDate)
		days = append(days, day)
	}

	for day := 0; day < p2pi.Days; day++ {
		cTicketOut[day] = make([][4]chan *Point2Point_Output, qcsLen)

		var traveldate, backdate string
		traveldate = days[0].AddDate(0, 0, day).Format("2006-01-02")
		if routLen == 2 {
			backdate = days[1].AddDate(0, 0, day).Format("2006-01-02")
		}

		for cs := range qcs.ConnStations {
			DepartStation := qcs.ConnStations[cs].DepartStation //Airport
			ArriveStation := qcs.ConnStations[cs].ArriveStation //Airport
			ConnectStations := qcs.ConnStations[cs].ConnStation //Airport

			R00 := *rout[0] //GoLeg1
			R10 := *rout[0] //GoLeg2
			R00.DepartCounty = []string{DepartStation}
			R00.ArriveCounty = ConnectStations
			R00.TravelDate = traveldate
			R10.DepartCounty = ConnectStations
			R10.ArriveCounty = []string{ArriveStation}
			R10.TravelDate = traveldate

			cTicketOut[day][cs][0] = make(chan *Point2Point_Output, 1) //GoLeg1+GoLeg2
			cTicketOut[day][cs][1] = make(chan *Point2Point_Output, 1) //GoLeg1+GoLeg2(Next Day)
			cTicketOut[day][cs][2] = make(chan *Point2Point_Output, 1) //BackLeg1+BackLeg2
			cTicketOut[day][cs][3] = make(chan *Point2Point_Output, 1) //BackLeg1(Next Day) +BackLeg2

			Fares := make([]*mysqlop.ResultMainBill, 4)    //(G) 00,01-->(合并) (B) 01,00-->(合并)
			Flights := make([][]cacheflight.FlightJSON, 4) //(G) 00,01-->(合并) (B) 01,00-->(合并)
			//这里存在玩笑式的技术:Flights本身并没有被调用,所以分配在栈,下次继续原地分配.
			//不可以[][]*cacheflight.FlightJSON (go1.8.3)
			NewRout := make([][]*mysqlop.Routine, 2)

			if routLen == 1 {
				Fares[0] = &mysqlop.ResultMainBill{Mainbill: fareTimes[cs][0].Fares[day][0]}
				Fares[1] = &mysqlop.ResultMainBill{Mainbill: fareTimes[cs][0].Fares[day][0]}
				Fares[2] = &mysqlop.ResultMainBill{Mainbill: fareTimes[cs][1].Fares[day][0]}
				Fares[3] = &mysqlop.ResultMainBill{Mainbill: fareTimes[cs][1].Fares[day+1][0]}

				Flights[0] = []cacheflight.FlightJSON{*flightTimes[cs][0][day]}
				Flights[1] = []cacheflight.FlightJSON{*flightTimes[cs][0][day]}
				Flights[2] = []cacheflight.FlightJSON{*flightTimes[cs][1][day]}
				Flights[3] = []cacheflight.FlightJSON{*flightTimes[cs][1][day+1]}

				NewRout[0] = []*mysqlop.Routine{&R00}
				NewRout[1] = []*mysqlop.Routine{&R10}

			} else { //routLen == 2
				if len(fareTimes[cs][0].Fares[day]) == 2 {
					Fares[0] = &mysqlop.ResultMainBill{Mainbill: append(fareTimes[cs][0].Fares[day][0], fareTimes[cs][0].Fares[day][1]...)}
					Fares[1] = &mysqlop.ResultMainBill{Mainbill: append(fareTimes[cs][0].Fares[day][0], fareTimes[cs][0].Fares[day+1][1]...)}
					Fares[2] = &mysqlop.ResultMainBill{Mainbill: append(fareTimes[cs][1].Fares[day][0], fareTimes[cs][1].Fares[day][1]...)}
					Fares[3] = &mysqlop.ResultMainBill{Mainbill: append(fareTimes[cs][1].Fares[day+1][0], fareTimes[cs][1].Fares[day][1]...)}

					Flights[0] = []cacheflight.FlightJSON{*flightTimes[cs][0][day], *flightTimes[cs][3][day]}
					Flights[1] = []cacheflight.FlightJSON{*flightTimes[cs][0][day], *flightTimes[cs][3][day+1]}
					Flights[2] = []cacheflight.FlightJSON{*flightTimes[cs][1][day], *flightTimes[cs][2][day]}
					Flights[3] = []cacheflight.FlightJSON{*flightTimes[cs][1][day+1], *flightTimes[cs][2][day]}
				} else if len(fareTimes[cs][0].Fares[day]) == 1 {
					Fares[0] = &mysqlop.ResultMainBill{Mainbill: fareTimes[cs][0].Fares[day][0]}
					Fares[1] = &mysqlop.ResultMainBill{Mainbill: fareTimes[cs][0].Fares[day][0]}
					Fares[2] = &mysqlop.ResultMainBill{Mainbill: fareTimes[cs][1].Fares[day][0]}
					Fares[3] = &mysqlop.ResultMainBill{Mainbill: fareTimes[cs][1].Fares[day+1][0]}

					Flights[0] = []cacheflight.FlightJSON{*flightTimes[cs][0][day]}
					Flights[1] = []cacheflight.FlightJSON{*flightTimes[cs][0][day]}
					Flights[2] = []cacheflight.FlightJSON{*flightTimes[cs][1][day]}
					Flights[3] = []cacheflight.FlightJSON{*flightTimes[cs][1][day+1]}
				}

				R01 := *rout[1] //BackLeg1
				R11 := *rout[1] //BackLeg2
				R01.DepartCounty = ConnectStations
				R01.ArriveCounty = []string{DepartStation}
				R01.TravelDate = backdate
				R11.DepartCounty = []string{ArriveStation}
				R11.ArriveCounty = ConnectStations
				R11.TravelDate = backdate
				NewRout[0] = []*mysqlop.Routine{&R00, &R01}
				NewRout[1] = []*mysqlop.Routine{&R10, &R11}
			}

			//注意NewRout的使用范围
			go Point2Point_V2(QuickShopping, dc, *Fares[0], Flights[0], DealID+1, p2pi, NewRout[0], false, true, cTicketOut[day][cs][0])
			if routLen == 2 {
				go Point2Point_V2(QuickShopping, dc, *Fares[1], Flights[1], DealID+2, p2pi, NewRout[0], false, true, cTicketOut[day][cs][1])
			} else {
				cTicketOut[day][cs][1] <- nullOutput
			}

			go Point2Point_V2(QuickShopping, dc, *Fares[2], Flights[2], DealID+3, p2pi, NewRout[1], true, false, cTicketOut[day][cs][2])
			go Point2Point_V2(QuickShopping, dc, *Fares[3], Flights[3], DealID+4, p2pi, NewRout[1], true, false, cTicketOut[day][cs][3])

			DealID += 4
		}
	}

	MakeTicket := func(output0, output1, output2, output3 *Point2Point_Output,
		chanOP chan *Point2Point_Output) {

		shoppingout := &Point2Point_Output{
			Result:  1,
			Segment: []*Point2Point_RoutineInfo2Segment{},
			Fare:    []*FareInfo{},
			Journey: []*JourneyInfo{}}

		if (output0.Result == 1 || output1.Result == 1) &&
			(output2.Result == 1 || output3.Result == 1) {

			//合并Flight Info
			output0.Segment = append(output0.Segment, output1.Segment...)
			output2.Segment = append(output2.Segment, output3.Segment...)
			shoppingout.Segment = append(output0.Segment, output2.Segment...)

			//合并Fare Info
			output0.Fare = append(output0.Fare, output1.Fare...)
			output2.Fare = append(output2.Fare, output3.Fare...)
			shoppingout.Fare = append(output0.Fare, output2.Fare...)

			//合并Journey Info
			output0.Journey = append(output0.Journey, output1.Journey...)
			output2.Journey = append(output2.Journey, output3.Journey...)
			//p2pi主要作用是中转链接时间
			shoppingout.Journey = MergeJourney_V1(p2pi, output0, output2)
			sort.Sort(shoppingout.Journey)
		}

		chanOP <- shoppingout
	}

	chanOutput := make([]chan *Point2Point_Output, p2pi.Days, p2pi.Days)

	for day := 0; day < p2pi.Days; day++ {
		chanOutput[day] = make(chan *Point2Point_Output)
		for cs := range qcs.ConnStations {
			go MakeTicket(
				<-cTicketOut[day][cs][0],
				<-cTicketOut[day][cs][1],
				<-cTicketOut[day][cs][2],
				<-cTicketOut[day][cs][3],
				chanOutput[day])
		}
	}

	for day := 0; day < p2pi.Days; day++ {
		shoppingout.ListShopping = append(shoppingout.ListShopping, <-chanOutput[day])
	}

	if p2pi.Days > 1 {
		shoppingout.Lower()
	}

	return
}

//对QuickTwoTicketDeal_V1的简单封装,用户热门旅游的单shopping/day输出
func QuickTwoTicketDeal_V2(
	af *cachestation.Agencies,
	p2pi *Point2Point_In,
	rout []*mysqlop.Routine,
	qcs *QCS,
	fareTimes [][2]*mysqlop.MutilDaysMainBill,
	flightTimes [][][]*cacheflight.FlightJSON,
	shoppingout chan *ListPoint2Point_Output) {
	//HotJounery的QuickShopping值为true && DebugControl==nil
	defer errorlog.DealRecoverLog()

	var out *ListPoint2Point_Output
	defer func() {
		shoppingout <- out
	}()

	out = QuickTwoTicketDeal_V1(nil, p2pi.Quick /*true*/, nil, p2pi, rout, qcs, fareTimes, flightTimes)
}

//#TODO 点到点查询，如果没缓存的话，通常走这里
//供应商数据资源分配,包含处理Quick***TicketDeal_V*
func AgencyDistribution_V1(
	result []byte, //源请求报文
	dc *DebugControl, //写Debug日志
	p2pi *Point2Point_In, //查询条件
) (outputDays *ListPoint2Point_Output) {

	rout := p2pi.Rout                    //入口时已经完成了p2pi.Rout
	qcs := RoutineCheckSelect_V2(result) //这里必须在没有票单的时候处理
	fareTimes, flightTimes := QueryFareAndFlighttime(0, p2pi, rout)

	//以下数据不同供应商是同一管道
	ADShoppingChanMB := make([]chan []*MB2RouteInfo, p2pi.Days)      //多天@快速shopping
	ADShoppingChanOut := make([]chan *Point2Point_Output, p2pi.Days) //多天@实时shopping

	if p2pi.Quick {
		for i := 0; i < p2pi.Days; i++ {
			ADShoppingChanMB[i] = make(chan []*MB2RouteInfo, len(cachestation.AgencyFrame))
		}
	} else {
		for i := 0; i < p2pi.Days; i++ {
			ADShoppingChanOut[i] = make(chan *Point2Point_Output, len(cachestation.AgencyFrame))
		}
	}
	var shoppingout chan *ListPoint2Point_Output
	record := 0
	ds := ""

	//这个接口是支持传多个注册公司进来。
	for _, office := range p2pi.Offices {
		ds += " " + cachestation.Office2DataSource[office]

	}
	//fmt.Println("Office2DS=", ds) //////

	//#TODO 遍历内存里面的AgencyFrame，看传进来的参数是否有在这里
	for _, af := range cachestation.AgencyFrame {

		if p2pi.Agency != "" && p2pi.Agency != af.Agency {
			continue
		}

		//#TODO 升哥2018-08-25加入
		if af.ShowShoppingName == "ES" && p2pi.BerthType != "Y" {
			continue
		}

		if !strings.Contains(ds, af.ShowShoppingName) {
			continue
		}

		//fmt.Println("Select DS=", af.ShowShoppingName, af.Agency)

		if af.Agency == "FB" && !qcs.Havefare {
			if len(p2pi.Flight) > 1 && //单程不做2票合并
				len(qcs.ConnStations) > 0 {

				var departCountry, arriveCountry string

				//查询出发城市和到达城市是否在缓存中
				if county, ok := cachestation.County[p2pi.Rout[0].DepartCounty[0]]; ok {
					departCountry = cachestation.CityCountry[county.City]
				}
				if county, ok := cachestation.County[p2pi.Rout[0].ArriveCounty[0]]; ok {
					arriveCountry = cachestation.CityCountry[county.City]
				}

				if departCountry != arriveCountry { //其实电商不适合2票接驳的缓存
					shoppingout = make(chan *ListPoint2Point_Output, 1)
					//shoppingout就是为了回调数据出去的
					//#TODO  QuickTwoTicketDeal_V2 查询两张票的吗
					go QuickTwoTicketDeal_V2(af, p2pi, p2pi.Rout, qcs, nil, nil, shoppingout)
				}
			} else {
				continue
			}
		} else { //其它供应商
			if af.SaveAs == "Cache" {
				go QuickOneTicketDeal_V1(af, ADShoppingChanMB, ADShoppingChanOut, p2pi.Quick, dc, fareTimes, flightTimes, p2pi, p2pi.Rout)
			} else {
				//ES 1E W5都是跑这里
				go QuickOneTicketDeal_V1(af, ADShoppingChanMB, ADShoppingChanOut, p2pi.Quick, dc, nil, nil, p2pi, p2pi.Rout)
			}
			record++
		}
	}

	shoppingout_record := make([]chan *Point2Point_Output, p2pi.Days)

	for day := 0; day < p2pi.Days; day++ {
		shoppingout_record[day] = make(chan *Point2Point_Output, len(cachestation.AgencyFrame))
		go AcceptP2P_V1(p2pi, rout, ADShoppingChanMB[day], ADShoppingChanOut[day], record, shoppingout_record[day])
	}

	outputDays = &ListPoint2Point_Output{}
	//outputDays.ListShopping
	for _, out := range shoppingout_record {
		tmpjs := <-out
		//fmt.Println("AgencyDistribution_V1 5, len=", len(tmpjs.Journey), "and k=", k, "and long=", len(shoppingout_record))
		outputDays.ListShopping = append(outputDays.ListShopping, tmpjs)
	}
	//fmt.Println("outputDays len=", len(outputDays.ListShopping))

	if shoppingout != nil { //存在2票合并情况
		twoout := <-shoppingout
		for i := range outputDays.ListShopping {
			if outputDays.ListShopping[i] == nil || outputDays.ListShopping[i].Fare == nil { //两票合并中出现了部分问题
				outputDays.ListShopping[i] = &Point2Point_Output{
					Segment: make([]*Point2Point_RoutineInfo2Segment, 0, 20),
					Fare:    make([]*FareInfo, 0, 20),
					Journey: make([]*JourneyInfo, 0, 20),
				}
			}
			if twoout.ListShopping[i] != nil {
				outputDays.ListShopping[i].Fare = append(outputDays.ListShopping[i].Fare, twoout.ListShopping[i].Fare...)
				outputDays.ListShopping[i].Segment = append(outputDays.ListShopping[i].Segment, twoout.ListShopping[i].Segment...)
				outputDays.ListShopping[i].Journey = append(outputDays.ListShopping[i].Journey, twoout.ListShopping[i].Journey...)
			}
		}
	}

	for _, shopping := range outputDays.ListShopping {
		if shopping != nil && len(shopping.Journey) > 0 {
			shopping.Result = 1
		}
	}

	return
}

/**************快速Shopping接口**************
WAPI_Point2Point_V*
Author： 王冬升 2015-07-08
Author： 王冬升 2016-05-08 2单程组合成双程
Author： 王冬升 2016-05-18 前主2张票单合并
Author: SKHuang 2018-08-05 wudy说要新增票单
******************************************/

//#TODO WAPI_QuickPoint2Point_V1 通过这个接口可以查出数据，shopping出结果()
var waitdeal time.Duration = time.Second / 4 ////0.25s

//未解决跨季度的加价计算:因为航空公司的季度计算是淡旺季度,不是时令季度.
//这接口估计就是我们做机票的时候调用到的最真实的数据了
func WAPI_QuickPoint2Point_V1(w http.ResponseWriter, r *http.Request) {

	defer errorlog.DealRecoverLog()

	r.ParseForm()

	result, _ := ioutil.ReadAll(r.Body)

	//现在	p2pi这个结构还要加上大人，小孩的人数。可以接受这两个参数进行处理

	var p2pi Point2Point_In

	//这里传入的参数是p2pi，也就是Point2Point_In 这个类型的。传出去应该是shoppingout 他是[]byte类型的
	if err := json.Unmarshal(result, &p2pi); err != nil {

		errorlog.WriteErrorLog("WAPI_QuickPoint2Point_V1 (1): " + err.Error())
		fmt.Fprint(w, bytes.NewBuffer(ShoppingErrOut))
		return
	}

	//#TODO 这里还需要对舱位进行处理。全部再特别返回出去
	//1.如果是Y舱的话，则可以去ES这个数据源拿；如果不是Y舱，则忽略掉数据源是ES的
	//不能在这里过滤，这样会影响到其他同时调用这个接口的请求。这样会导致数据没办法一致性


	//需要传入出发地目的地信息
	if len(p2pi.Flight) == 0 {
		fmt.Fprint(w, bytes.NewBuffer(ShoppingErrOut))
		return
	}

	//Rout这里是多票单合并,直接传输中转地,如不这里,分析后的中转地会是错误的；；；；；
	//如果Rout不传的话，则这里会调用FlightLegs2Routine(p2pi.Flight) 这个函数来给Route赋值
	//#TODO  FlightLegs2Routine（）这个函数


	//如果没有传入Rout的话，我们可以自己输入的Flight去制作一个Route
	if len(p2pi.Rout) == 0 {
		p2pi.Rout = FlightLegs2Routine(p2pi.Flight) //rout是真实取获取的行段
	}

	routLen := len(p2pi.Rout)

	//做的只是单程和往返程。其实现在会默认1天。。。。两个月60天
	//如果查询的段数大于2或者等于0 或者传入的天数大于60天啊等于0，都是限制的。

	if routLen > 2 || routLen == 0 ||
		p2pi.Days < 0 || p2pi.Days > 60 {
		fmt.Fprint(w, bytes.NewBuffer(ShoppingErrOut))
		return
	}

	//获取第一段的出发地。将出发地的整理成数组。并算出出发地的个数
	lenDepartStation := len(strings.Split(p2pi.Flight[0].DepartStation, " ")) //出发地数组长度（输入是有空格的，，，s）
	lenArriveStation := len(strings.Split(p2pi.Flight[0].ArriveStation, " ")) //热门旅游是后来硬扭进来的.本来有专门的接口.

	if p2pi.Days == 0 || //!p2pi.Quick ||
		lenDepartStation > 1 || lenArriveStation > 1 {
		p2pi.Days = 1
	}

	if p2pi.Days > 1 || lenDepartStation > 1 || lenArriveStation > 1 {
		p2pi.Debug = false //日历功能禁止调试
	}

	//出发地与目的地间只存在一个"多",这是排版的需要.
	if lenDepartStation > 1 && lenArriveStation > 1 {
		fmt.Fprint(w, bytes.NewBuffer(ShoppingErrOut))
		return
	}

	//最多5个，太多的话前端显示补了（这和票单那块不一样。和产品系统里面的票单有区别）
	if lenDepartStation > 5 || lenArriveStation > 5 {
		fmt.Fprint(w, bytes.NewBuffer(ShoppingErrOut))
		return
	}

	//全部都是开口程的流程。。
	if routLen == 2 && p2pi.Rout[0].Trip == 0 { //目前不操作缺口程数据
		fmt.Fprint(w, bytes.NewBuffer(ShoppingErrOut))
		return
	}

	//如果Offices 空，则默认其为3007F
	if len(p2pi.Offices) == 0 {
		p2pi.Offices = []string{"3007F"}
	}

	//UserKind1 空，则默认其为INCU(这个在LR里面，好像是内部员工的意思)---用户折扣代码，。。（升哥说INCU是折扣代码）
	if p2pi.UserKind1 == "" {
		p2pi.UserKind1 = "INCU"
	}

	//传入了对应的数据源。有时候会指定返回哪一个数据源。这里一般做测试专用。正常情况下都不会传Agency，除非为了测试使用
	//如果不传的话，则会去拿所有的数据源
	if p2pi.Agency != "" {
		la := 0
		//后面的话会往cachestation.AgencyFrame加起来；（cachestation.AgencyFrame 一开始会给出两个默认，后面会根据数据库配置表，继续增加）

		for ; la < len(cachestation.AgencyFrame); la++ {

			if cachestation.AgencyFrame[la].Agency == p2pi.Agency {
				break
			}
		}

		//如果缓存里面的cachestation.AgencyFrame 长度为0，则代表不合理。直接返回错误信息。或者说在这里通过遍历之后，最终la和cathestation.AgencyFrame一样大，那就是不对劲了。就是说找不到对应的Agency
		if la == len(cachestation.AgencyFrame) {
			fmt.Fprint(w, bytes.NewBuffer(ShoppingErrOut))
			return
		}
	}

	//中转总时间保持在20-1440分钟
	if p2pi.ConnMinutes < 20 {
		p2pi.ConnMinutes = 20
	} else if p2pi.ConnMinutes > 1440 {
		p2pi.ConnMinutes = 1440
	}

	//绕航率 保持在100-250之间
	//绕航率=====
	if p2pi.DeviationRate < 100 {
		p2pi.DeviationRate = 100
	} else if p2pi.DeviationRate > 250 {
		p2pi.DeviationRate = 250
	}

	/***航司联盟控制2016-02-15***/
	//一般来说人家没有传入
	if index := mysqlop.AllianceIndex(p2pi.Alliance); index < 99 && len(p2pi.Airline) == 0 {
		p2pi.Airline = cachestation.AllianceList[index]
	}

	var (
		shoppingout []byte
		//      5/CANBJS2018-08-23-DSICIKDMV2018-08-29-CZHU-222Y      这是一个demo（以及排序了.......为什么去函数里面看没有呢）
		journeyline = strconv.Itoa(p2pi.Days) + "/" + JourneyLine(&p2pi) //这里的journeyline 是查询的路线，航司  等的整合，组合成字符串，便于下次进行查询。缓存
	)

	if p2pi.Quick {
		//  Q/5/CANBJS2018-08-23-DSICIKDMV2018-08-29-CZHU-222Y
		journeyline = "Q/" + journeyline
	}

	var dc *DebugControl

	//开启调试模式的话，则会将对应的数据打印出来，开启调试模式
	if p2pi.Debug {
		dc = &DebugControl{}
		dc.Init(journeyline)
	}

	fmt.Printf("组合后的参数 %+v", p2pi)

	//缓存过期在特殊进程里处理,这里拿到就是成功(每天要清理一次,去除失败的shopping卡着)
	SR1Lock.RLock()
	//这里传入journeyline去缓存里面获取
	sr, b := SR1[journeyline]
	SR1Lock.RUnlock()

	//拿到了缓存
	if b {

		//这个42代表什么意思
		//获取最多超过多少秒钟，每次等待的时间大概是10s。
		for i := 0; sr.Shopping == nil && i < 42; i++ {
			time.Sleep(waitdeal) // 0.25s
		}
		if sr.Shopping != nil {
			shoppingout = sr.Shopping //将sr.shopping传出去。就是我们要的结果
		} else {
			shoppingout = ShoppingTimeOut
		}

	} else if lenDepartStation > 1 || lenArriveStation > 1 {

		//制作的是热门景点的票价。-----（如果输入的是1个景点）

		sr := &ShoppingResult{time.Now(), nil} //这里是为了表明Shopping处理中

		SR1Lock.Lock()
		SR1[journeyline] = sr
		SR1Lock.Unlock()

		//HotJourney_V1      制作的是热门景点的票价
		//将p2pi 传到HotJourney_V1 这个函数里面去。将结果存到listOutput。接着处理listOutput
		listOutput := HotJourney_V1(&p2pi)

		if shoppingout = errorlog.Make_JSON_GZip(listOutput); shoppingout == nil {
			shoppingout = ShoppingErrOut
		}
		sr.Shopping = shoppingout

		SR1Lock.Lock()
		delete(SR1, journeyline)
		SR1Lock.Unlock()

	} else {

		//正常的票价。
		sr := &ShoppingResult{time.Now(), nil} //这里是为了表明Shopping处理中

		SR1Lock.Lock()
		SR1[journeyline] = sr
		SR1Lock.Unlock()

		defer func() {
			SR1Lock.Lock()
			delete(SR1, journeyline) //因为不缓存第1次结果,前面的缓存是为了不重复在做的工作
			SR1Lock.Unlock()
		}()

		output := nullOutput

		outputDays := AgencyDistribution_V1(result, dc, &p2pi)

		if p2pi.Days == 1 {

			//代表不是日历的。如果不是1的话，就是列表结果
			if len(outputDays.ListShopping) > 0 && outputDays.ListShopping[0] != nil {
				output = outputDays.ListShopping[0]
			}

			if output.Result == 3 {
				shoppingout = ShoppingTimeOut

			} else if output.Result == 100 {
				shoppingout = ShoppingNULL
			} else {
				if shoppingout = errorlog.Make_JSON_GZip(output); shoppingout == nil {
					shoppingout = ShoppingErrOut
				}
			}
		} else {
			if shoppingout = errorlog.Make_JSON_GZip(outputDays); shoppingout == nil {
				shoppingout = ShoppingErrOut
			}
		}
		sr.Shopping = shoppingout

	}

	//fmt.Fprint(w, bytes.NewBuffer(shoppingout))
	//fmt.Fprint(w, errorlog.Make_JSON_GZip_Reader(shoppingout))
	fmt.Fprint(w, bytes.NewBuffer(shoppingout))

}

/**************热门景点Shopping接口***********/

func HotJourneyOneTicketDeal_V1( //DealID
	af *cachestation.Agencies, //供应商资料
	quickout []chan []*MB2RouteInfo,
	shoppingout []chan *Point2Point_Output,
	stationHaveFare []*Point2Point_In) {

	var (
		routLen     = len(stationHaveFare[0].Flight)
		havefareLen = len(stationHaveFare)
	)

	faresIndex := make(map[string]int, havefareLen*3)

	faresIndexAdd := func(DepartCounty, ArriveCounty []string, fareindex int) {
		for _, depart := range DepartCounty {
			for _, arrive := range ArriveCounty {
				faresIndex[depart+arrive] = fareindex
				faresIndex[arrive+depart] = fareindex
			}
		}
	}

	var rout []*mysqlop.Routine
	var fares = make([]*mysqlop.ResultMainBill, havefareLen)        //[City]
	var flightTimes = make([][]cacheflight.FlightJSON, havefareLen) //[City][Go/Back]

	for i, p2pi_hot := range stationHaveFare {
		fares[i] = &mysqlop.ResultMainBill{Mainbill: make([]*mysqlop.MainBill, 0, 150)} //航班记录都是偏大的
		flightTimes[i] = make([]cacheflight.FlightJSON, routLen)
		flightTimes[i][0] = cacheflight.FlightJSON{}
		if routLen == 2 {
			flightTimes[i][1] = cacheflight.FlightJSON{}
		}

		dep := cachestation.CityCounty[p2pi_hot.Flight[0].DepartStation]
		if len(dep) == 0 {
			dep = append(dep, p2pi_hot.Flight[0].DepartStation)
		}

		arr := cachestation.CityCounty[p2pi_hot.Flight[0].ArriveStation]
		if len(arr) == 0 {
			arr = append(arr, p2pi_hot.Flight[0].ArriveStation)
		}

		faresIndexAdd(dep, arr, i)

		if i == 0 {
			rout = p2pi_hot.Rout //FlightLegs2Routine(stationHaveFare[0].Flight)
		} else {
			rout[0].DepartCounty = append(rout[0].DepartCounty, p2pi_hot.Rout[0].DepartCounty...)
			rout[0].ArriveCounty = append(rout[0].ArriveCounty, p2pi_hot.Rout[0].ArriveCounty...)
			if routLen == 2 {
				rout[1].DepartCounty = append(rout[1].DepartCounty, p2pi_hot.Rout[1].DepartCounty...)
				rout[1].ArriveCounty = append(rout[1].ArriveCounty, p2pi_hot.Rout[1].ArriveCounty...)
			}
		}
	}

	faretimes, flighttimes := QueryFareAndFlighttime(0, stationHaveFare[0], rout)

	for _, fare := range faretimes.Fares[0] {
		for _, mb := range fare {
			fi := faresIndex[mb.Springboard+mb.Destination]
			fares[fi].Mainbill = append(fares[fi].Mainbill, mb)
		}
	}

	for gb, routFlight := range flighttimes {
		for _, leg := range routFlight[0].Route {
			legLen := len(leg.FI[0].Legs)
			fi := faresIndex[leg.FI[0].Legs[0].DS+leg.FI[0].Legs[legLen-1].AS]
			flightTimes[fi][gb].Route = append(flightTimes[fi][gb].Route, leg)
		}
	}

	for i := range stationHaveFare {
		if len(fares[i].Mainbill) > 0 && (len(flightTimes[i][0].Route) > 0 || !stationHaveFare[i].Quick) &&
			(routLen == 1 || len(flightTimes[i][1].Route) > 0 || !stationHaveFare[i].Quick) {

			//下面引进的是城市
			go Point2Point_V3(stationHaveFare[i].Quick, nil, *fares[i], flightTimes[i], 0, stationHaveFare[i], nil, false, false, af, quickout[i], shoppingout[i])
		}
	}
}

// #TODO HotJourneyTwoTicketDeal_V1 蒙
//这里的2张出票是没办法使用QueryFareAndFlighttimeWithConns整理的,因为是1天多QCS
func HotJourneyTwoTicketDeal_V1(

	stationNotFare []*Point2Point_In, //stationNotFare,qcss长度是一样的
	qcss []*QCS,
	listOutputChan chan ListPoint2Point_Output) {

	var (
		routLen     = len(stationNotFare[0].Flight)
		havefareLen = len(stationNotFare)
		listOutput  = ListPoint2Point_Output{ListShopping: make([]*Point2Point_Output, 0, havefareLen)}
	)

	defer func() {
		listOutputChan <- listOutput
	}()

	stationNotFare[0].Days = 2 //这里是因为2张出票日期退后一天导致的.

	var faresIndex = make(map[string][]int, havefareLen*3)                  //string==Routine,[]int==Diff City/County
	var rout = FlightLegs2Routine(stationNotFare[0].Flight)                 //routine模板
	var goFares = make([][2]*mysqlop.ResultMainBill, havefareLen)           //goRoutine [Diff City][2Days]
	var goFlightTimes = make([][2][]*cacheflight.FlightJSON, havefareLen)   //goRoutine [Diff City][2Days][Go/Back]
	var backFares = make([][2]*mysqlop.ResultMainBill, havefareLen)         //backRoutine [Diff City][2Days]
	var backFlightTimes = make([][2][]*cacheflight.FlightJSON, havefareLen) //backRoutine [Diff City][2Days][Go/Back]
	var goRoutine = make(map[string][]string, havefareLen*3)                //去程出发地与中转地string==出发地,[]string中转地
	var backRoutine = make(map[string][]string, havefareLen*3)              //回程目的地与中转地string==目的地,[]string中转地

	faresIndexAdd := func(DepartCounty string, ArriveCounty []string, fareindex int) {
		for _, arrive := range ArriveCounty {
			if fi, ok := faresIndex[DepartCounty+arrive]; !ok {
				faresIndex[DepartCounty+arrive] = []int{fareindex}
			} else {
				faresIndex[DepartCounty+arrive] = append(fi, fareindex)
			}
			if fi, ok := faresIndex[arrive+DepartCounty]; !ok {
				faresIndex[arrive+DepartCounty] = []int{fareindex}
			} else {
				faresIndex[arrive+DepartCounty] = append(fi, fareindex)
			}
		}
	}

	routineAdd := func(routine *map[string][]string, DepartStation string, ConnStation []string) {
		if stations, ok := (*routine)[DepartStation]; !ok {
			(*routine)[DepartStation] = ConnStation
		} else {
			sLen := len(stations)
			for _, conn := range ConnStation {
				i := 0
				for ; i < sLen; i++ {
					if conn == stations[i] {
						break
					}
				}
				if i == sLen {
					stations = append(stations, conn)
				}
			}

			(*routine)[DepartStation] = stations
		}
	}

	faresAdd := func(gbFares [][2]*mysqlop.ResultMainBill, faresdays *mysqlop.MutilDaysMainBill) {
		for day, gbListfares := range faresdays.Fares {
			for _, listFares := range gbListfares {
				for _, fare := range listFares {
					fis := faresIndex[fare.Springboard+fare.Destination]
					for _, fi := range fis {
						gbFares[fi][day].Mainbill = append(gbFares[fi][day].Mainbill, fare)
					}
				}
			}
		}
	}

	flightTimesAdd := func(gbFlight [][2][]*cacheflight.FlightJSON, flightTimes [][]*cacheflight.FlightJSON) {
		for gb, flightsdays := range flightTimes {
			for day, flights := range flightsdays {
				for _, flight := range flights.Route {
					fl := len(flight.FI[0].Legs)
					fis := faresIndex[flight.FI[0].Legs[0].DS+flight.FI[0].Legs[fl-1].AS]
					for _, fi := range fis {
						gbFlight[fi][day][gb].Route = append(gbFlight[fi][day][gb].Route, flight)
					}
				}
			}
		}
	}

	for i, qcs := range qcss {
		goFares[i][0] = &mysqlop.ResultMainBill{Mainbill: make([]*mysqlop.MainBill, 0, 150)} //航班记录都是偏大的
		goFares[i][1] = &mysqlop.ResultMainBill{Mainbill: make([]*mysqlop.MainBill, 0, 150)}
		backFares[i][0] = &mysqlop.ResultMainBill{Mainbill: make([]*mysqlop.MainBill, 0, 150)}
		backFares[i][1] = &mysqlop.ResultMainBill{Mainbill: make([]*mysqlop.MainBill, 0, 150)}
		goFlightTimes[i][0] = make([]*cacheflight.FlightJSON, routLen)
		goFlightTimes[i][1] = make([]*cacheflight.FlightJSON, routLen)
		backFlightTimes[i][0] = make([]*cacheflight.FlightJSON, routLen)
		backFlightTimes[i][1] = make([]*cacheflight.FlightJSON, routLen)

		goFlightTimes[i][0][0] = &cacheflight.FlightJSON{}
		goFlightTimes[i][1][0] = &cacheflight.FlightJSON{}
		backFlightTimes[i][0][0] = &cacheflight.FlightJSON{}
		backFlightTimes[i][1][0] = &cacheflight.FlightJSON{}

		if routLen == 2 {
			goFlightTimes[i][0][1] = &cacheflight.FlightJSON{}
			goFlightTimes[i][1][1] = &cacheflight.FlightJSON{}
			backFlightTimes[i][0][1] = &cacheflight.FlightJSON{}
			backFlightTimes[i][1][1] = &cacheflight.FlightJSON{}
		}

		for _, conn := range qcs.ConnStations {
			faresIndexAdd(conn.DepartStation, conn.ConnStation, i)
			faresIndexAdd(conn.ArriveStation, conn.ConnStation, i)

			routineAdd(&goRoutine, conn.DepartStation, conn.ConnStation)
			routineAdd(&backRoutine, conn.ArriveStation, conn.ConnStation)
		}
	}

	var goFaresChan = make([]chan *mysqlop.MutilDaysMainBill, len(goRoutine))
	var goFlightTimesChan = make([]chan [][]*cacheflight.FlightJSON, len(goRoutine))
	var backFaresChan = make([]chan *mysqlop.MutilDaysMainBill, len(backRoutine))
	var backFlightTimesChan = make([]chan [][]*cacheflight.FlightJSON, len(backRoutine))
	var RC = 0 //RoutineCount

	for DepartCounty, ConnStation := range goRoutine {
		goFaresChan[RC] = make(chan *mysqlop.MutilDaysMainBill)
		goFlightTimesChan[RC] = make(chan [][]*cacheflight.FlightJSON)

		tmp_rout := make([]*mysqlop.Routine, 0, routLen)
		tmp_rout0 := *rout[0]
		tmp_rout0.DepartCounty = []string{DepartCounty}
		tmp_rout0.ArriveCounty = ConnStation
		tmp_rout = append(tmp_rout, &tmp_rout0)

		if routLen == 2 {
			tmp_rout1 := *rout[1]
			tmp_rout1.DepartCounty = ConnStation
			tmp_rout1.ArriveCounty = []string{DepartCounty}
			tmp_rout = append(tmp_rout, &tmp_rout1)
		}

		go QueryFareAndFlighttime_V2(0,
			stationNotFare[0],
			tmp_rout,
			goFaresChan[RC],
			goFlightTimesChan[RC])

		RC++
	}

	RC = 0
	for ArriveCounty, ConnStation := range backRoutine {
		backFaresChan[RC] = make(chan *mysqlop.MutilDaysMainBill)
		backFlightTimesChan[RC] = make(chan [][]*cacheflight.FlightJSON)

		tmp_rout := make([]*mysqlop.Routine, 0, routLen)

		tmp_rout0 := *rout[0]
		tmp_rout0.DepartCounty = ConnStation
		tmp_rout0.ArriveCounty = []string{ArriveCounty}
		tmp_rout = append(tmp_rout, &tmp_rout0)

		if routLen == 2 {
			tmp_rout1 := *rout[1]
			tmp_rout1.DepartCounty = []string{ArriveCounty}
			tmp_rout1.ArriveCounty = ConnStation
			tmp_rout = append(tmp_rout, &tmp_rout1)
		}

		go QueryFareAndFlighttime_V2(0,
			stationNotFare[0],
			tmp_rout,
			backFaresChan[RC],
			backFlightTimesChan[RC])

		RC++
	}

	for i := range goFaresChan {
		faresAdd(goFares, <-goFaresChan[i])
		flightTimesAdd(goFlightTimes, <-goFlightTimesChan[i])
	}

	for i := range backFaresChan {
		faresAdd(backFares, <-backFaresChan[i])
		flightTimesAdd(backFlightTimes, <-backFlightTimesChan[i])
	}

	stationNotFare[0].Days = 1 //恢复当天数据
	nullFare := mysqlop.ListMainBill{}
	nullFlightTime := &cacheflight.FlightJSON{}
	//nullMainBill := &mysqlop.ResultMainBill{}
	shoppingoutChan := make(chan *ListPoint2Point_Output)
	countChan := 0
	for city, qcs := range qcss {

		if len(goFares[city][0].Mainbill) == 0 ||
			len(goFlightTimes[city][0][0].Route) == 0 ||
			len(backFlightTimes[city][0][0].Route) == 0 ||
			routLen == 2 &&
				(len(goFlightTimes[city][0][1].Route) == 0 ||
					len(backFlightTimes[city][0][1].Route) == 0) {
			continue
		}

		qcsLen := len(qcs.ConnStations)
		fareTimes := make([][2]*mysqlop.MutilDaysMainBill, qcsLen)
		flightTimes := make([][][]*cacheflight.FlightJSON, qcsLen)

		for i := range qcs.ConnStations {
			fareTimes[i][0] = &mysqlop.MutilDaysMainBill{
				Fares: [][2]mysqlop.ListMainBill{
					{goFares[city][0].Mainbill, nullFare},
					{goFares[city][1].Mainbill, nullFare}}}

			fareTimes[i][1] = &mysqlop.MutilDaysMainBill{
				Fares: [][2]mysqlop.ListMainBill{
					{backFares[city][0].Mainbill, nullFare},
					{backFares[city][1].Mainbill, nullFare}}}

			flightTimes[i] = make([][]*cacheflight.FlightJSON, routLen*2)
			flightTimes[i][0] = []*cacheflight.FlightJSON{goFlightTimes[city][0][0], nullFlightTime}
			flightTimes[i][1] = []*cacheflight.FlightJSON{backFlightTimes[city][0][0], backFlightTimes[city][1][0]}
			if routLen == 2 {
				flightTimes[i][2] = []*cacheflight.FlightJSON{backFlightTimes[city][0][1], nullFlightTime}
				flightTimes[i][3] = []*cacheflight.FlightJSON{goFlightTimes[city][0][1], goFlightTimes[city][1][1]}
			}
		}
		countChan++
		go QuickTwoTicketDeal_V2(cachestation.AgencyFrame[0], stationNotFare[0], rout, qcs, fareTimes, flightTimes, shoppingoutChan)
	}

	for i := 0; i < countChan; i++ { //合理如果shoppingoutChan为nil会时错误的.
		listOutput.ListShopping = append(listOutput.ListShopping, (<-shoppingoutChan).ListShopping[0])
	}
}

//AgencyDistribution的HotJourney版本
//传入基本信息，输出结果
func HotJourney_V1(p2pi *Point2Point_In) (outputDays ListPoint2Point_Output) {

	//p2pi.Flight[0]里面的DepartStation和ArriveStation，也就是出发机场和到达机场其实都是可输入多数据的，中间以空格分割
	departStations := strings.Split(p2pi.Flight[0].DepartStation, " ")
	arriveStations := strings.Split(p2pi.Flight[0].ArriveStation, " ")
	HotCount := 0 //len(arriveStations)

	if len(departStations) > 1 {
		HotCount = len(departStations)

		for i := 0; i < HotCount; {
			if len(departStations[i]) == 0 {
				departStations[i], departStations[HotCount-1] = departStations[HotCount-1], departStations[i]
				HotCount--
			} else {
				i++
			}
		}

		if HotCount != len(departStations) {
			departStations = departStations[:HotCount]
		}
	} else {
		HotCount = len(arriveStations)
		for i := 0; i < HotCount; {
			if len(arriveStations[i]) == 0 {
				arriveStations[i], arriveStations[HotCount-1] = arriveStations[HotCount-1], arriveStations[i]
				HotCount--
			} else {
				i++
			}
		}

		if HotCount != len(arriveStations) {
			arriveStations = arriveStations[:HotCount]
		}
	}

	p2pi_stationsHot := make([]*Point2Point_In, 0, HotCount)
	p2pi_chansQcs := make([]chan *QCS, 0, HotCount)
	routLen := len(p2pi.Flight)

	for _, depart := range departStations { //City
		for _, arrive := range arriveStations { //City
			if depart == arrive {
				continue
			}

			p2pi_hot := *p2pi
			if routLen == 1 {
				f1 := *p2pi.Flight[0]
				f1.DepartStation = depart
				f1.ArriveStation = arrive
				p2pi_hot.Flight = []*Point2Point_Flight{&f1}
			} else {
				f1 := *p2pi.Flight[0]
				f2 := *p2pi.Flight[1]
				f1.DepartStation = depart
				f1.ArriveStation = arrive
				f2.DepartStation = arrive
				f2.ArriveStation = depart
				p2pi_hot.Flight = []*Point2Point_Flight{&f1, &f2}
			}

			p2pi_hot.Rout = FlightLegs2Routine(p2pi_hot.Flight) //20161129 这里是为了一起获取Fares & Flights.
			p2pi_stationsHot = append(p2pi_stationsHot, &p2pi_hot)
			p2pi_chan := make(chan *QCS)
			p2pi_chansQcs = append(p2pi_chansQcs, p2pi_chan)

			go RoutineCheckSelect_V3(&p2pi_hot, p2pi_chan)
		}
	}

	stationHaveFare := make([]*Point2Point_In, 0, HotCount) //直接存在Fare的Shopping
	stationNotFare := make([]*Point2Point_In, 0, HotCount)  //中转Fare的Shopping
	stationQCSs := make([]*QCS, 0, HotCount)                //stationNotFare对应的QCS

	for i := range p2pi_chansQcs {
		qcs := <-p2pi_chansQcs[i]

		if qcs.Havefare {
			stationHaveFare = append(stationHaveFare, p2pi_stationsHot[i])
		} else if len(qcs.ConnStations) > 0 {
			stationNotFare = append(stationNotFare, p2pi_stationsHot[i])
			stationQCSs = append(stationQCSs, qcs)
		}
	}

	//以下数据相同供应商是同一管道
	var shoppingout chan ListPoint2Point_Output
	LenHaveFare := len(stationHaveFare)
	ADShoppingChanMB := make([]chan []*MB2RouteInfo, LenHaveFare)      //多目的地@快速shopping
	ADShoppingChanOut := make([]chan *Point2Point_Output, LenHaveFare) //多目的地@实时shopping
	if p2pi.Quick {
		for i := 0; i < LenHaveFare; i++ {
			ADShoppingChanMB[i] = make(chan []*MB2RouteInfo, len(cachestation.AgencyFrame))
		}
	} else {
		for i := 0; i < LenHaveFare; i++ {
			ADShoppingChanOut[i] = make(chan *Point2Point_Output, len(cachestation.AgencyFrame))
		}
	}

	oneTicketFare := append(stationHaveFare, stationNotFare...)
	record := 0
	for _, af := range cachestation.AgencyFrame {
		if p2pi.Quick && af.Agency != "FB" && af.SaveAs == "Cache" {
			continue //所有公共缓存只进入一次处理
		}
		if af.Agency == "FB" && len(stationNotFare) > 0 {
			if len(p2pi.Flight) > 1 {
				var departCountry, arriveCountry string
				if county, ok := cachestation.County[p2pi.Rout[0].DepartCounty[0]]; ok {
					departCountry = cachestation.CityCountry[county.City]
				}
				if county, ok := cachestation.County[p2pi.Rout[0].ArriveCounty[0]]; ok {
					arriveCountry = cachestation.CityCountry[county.City]
				}

				if departCountry != arriveCountry { //其实电商不适合2票接驳的缓存
					shoppingout = make(chan ListPoint2Point_Output)
					go HotJourneyTwoTicketDeal_V1(stationNotFare, stationQCSs, shoppingout)
				}
			} else {
				continue
			}
		} else { //其它供应商
			record++
			go HotJourneyOneTicketDeal_V1(af, ADShoppingChanMB, ADShoppingChanOut, oneTicketFare)
		}
	}

	shoppingout_record := make([]chan *Point2Point_Output, len(oneTicketFare))
	rout := FlightLegs2Routine(p2pi.Flight) //这里只时被简单使用到AccessSeazon
	for day := 0; day < len(oneTicketFare); day++ {
		shoppingout_record[day] = make(chan *Point2Point_Output, len(cachestation.AgencyFrame))
		go AcceptP2P_V1(p2pi, rout, ADShoppingChanMB[day], ADShoppingChanOut[day], record, shoppingout_record[day])
	}

	for _, out := range shoppingout_record {
		outputDays.ListShopping = append(outputDays.ListShopping, <-out)
	}

	if shoppingout != nil { //存在2票合并情况
		twoout := <-shoppingout
		for i := range outputDays.ListShopping {
			outputDays.ListShopping[i].Fare = append(outputDays.ListShopping[i].Fare, twoout.ListShopping[i].Fare...)
			outputDays.ListShopping[i].Segment = append(outputDays.ListShopping[i].Segment, twoout.ListShopping[i].Segment...)
			outputDays.ListShopping[i].Journey = append(outputDays.ListShopping[i].Journey, twoout.ListShopping[i].Journey...)
		}
	}

	for _, shopping := range outputDays.ListShopping {
		if shopping != nil && len(shopping.Journey) > 0 {
			shopping.Result = 1
		}
	}

	return
}

//这个方法看起来和WAPI_QuickPoint2Point_V1 有点类似。是否只是逻辑不一样而已
func WAPI_HotJourneyPoint2Point_V1(w http.ResponseWriter, r *http.Request) {

	defer errorlog.DealRecoverLog()

	r.ParseForm()
	result, _ := ioutil.ReadAll(r.Body)

	var p2pi Point2Point_In
	if err := json.Unmarshal(result, &p2pi); err != nil {
		errorlog.WriteErrorLog("WAPI_HotJourneyPoint2Point_V1 (1): " + err.Error())
		fmt.Fprint(w, bytes.NewBuffer(ShoppingErrOut))
		return
	}
	if len(p2pi.Flight) == 0 {
		fmt.Fprint(w, bytes.NewBuffer(ShoppingErrOut))
		return
	}

	if len(p2pi.Rout) == 0 {
		p2pi.Rout = FlightLegs2Routine(p2pi.Flight) //rout是真实取获取的行段
	}
	routLen := len(p2pi.Rout)

	if routLen > 2 || routLen == 0 {
		fmt.Fprint(w, bytes.NewBuffer(ShoppingErrOut))
		return
	}

	//出发地与目的地间只存在一个"多",这是排版的需要.
	if len(strings.Split(p2pi.Flight[0].DepartStation, " ")) > 1 &&
		len(strings.Split(p2pi.Flight[0].ArriveStation, " ")) > 1 {
		fmt.Fprint(w, bytes.NewBuffer(ShoppingErrOut))
		return
	}

	p2pi.Days = 1
	p2pi.Debug = false
	p2pi.Quick = true

	if len(p2pi.Offices) == 0 {
		p2pi.Offices = []string{"3007F"}
	}

	if p2pi.UserKind1 == "" {
		p2pi.UserKind1 = "INCU"
	}

	if p2pi.ConnMinutes < 20 {
		p2pi.ConnMinutes = 20
	} else if p2pi.ConnMinutes > 1440 {
		p2pi.ConnMinutes = 1440
	}

	if p2pi.DeviationRate < 100 {
		p2pi.DeviationRate = 100
	} else if p2pi.DeviationRate > 250 {
		p2pi.DeviationRate = 250
	}

	/***航司联盟控制2016-02-15***/
	if index := mysqlop.AllianceIndex(p2pi.Alliance); index < 99 && len(p2pi.Airline) == 0 {
		p2pi.Airline = cachestation.AllianceList[index]
	}

	var (
		shoppingout []byte
		journeyline = "HJ/" + JourneyLine(&p2pi)
	)

	//缓存过期在特殊进程里处理,这里拿到就是成功(特殊进程5分钟处理一次)
	SR1Lock.RLock()
	sr, b := SR1[journeyline]
	SR1Lock.RUnlock()

	if b {
		for i := 0; sr.Shopping == nil && i < 42; i++ {
			time.Sleep(waitdeal)
		}
		if sr.Shopping != nil {
			shoppingout = sr.Shopping
		} else {
			shoppingout = ShoppingTimeOut
		}
	} else {

		sr := &ShoppingResult{time.Now(), nil} //这里是为了表明Shopping处理中

		SR1Lock.Lock()
		SR1[journeyline] = sr
		SR1Lock.Unlock()

		listOutput := HotJourney_V1(&p2pi)

		if shoppingout = errorlog.Make_JSON_GZip(listOutput); shoppingout == nil {
			shoppingout = ShoppingErrOut

		}
		sr.Shopping = shoppingout

		SR1Lock.Lock()
		delete(SR1, journeyline)
		SR1Lock.Unlock()
	}

	fmt.Fprint(w, bytes.NewBuffer(shoppingout))
}

//########################################################
var waitSecond = 10 * time.Second

type sWait struct {
	GetCount int
	Shopping chan []byte //ListPoint2Point_Output
}

type sOutAgents struct {
	AirAgents int //多少个供应商输出多少次
	Count     int //已经处理的次数
	Days      int //单天和多天处理不一样
	//Quick     bool      //快速一否处理不一样
	time         time.Time                   //建立时间
	dealIn       chan ListPoint2Point_Output //接受sProduction的传入
	ListShopping [][]byte                    //[]ListPoint2Point_Output
	mutex        sync.RWMutex                //确保ListWait读写成功
	ListWait     []*sWait
}

var AgentsShoppingOut = struct {
	mutex sync.RWMutex
	data  map[string]*sOutAgents
}{
	data: make(map[string]*sOutAgents, 20000),
}
