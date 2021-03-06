package webapi

import (
	"bytes"
	"cachestation"
	"encoding/json"
	"errorlog"
	//"errors"
	"fmt"
	"io/ioutil"
	"mysqlop"
	"net/http"
	"strconv"
	"strings"
	"time"
)

//调用了WAPI_DiscountCodeV1之后返回来的结果(这三个最后是和折扣代码详情那里类似的)
/**
折扣查询输出的结果
*/

type DiscountCode_Output struct {
	Model_3         []*DC_Model_3      `json:"Model"`           //每个office可以使用的折扣代码            数据源
	SegDiscountCode []*SegDiscountCode `json:"SegDiscountCode"` //有效的折扣代码                (航班折扣信息)
	ExceptCode []*SegDiscountCode `json:"ExceptCode"` //排除掉的折扣代码
}

/***DiscountCode查询服务***/
type DC_Model_3 struct {
	User string `json:"User"`
	*cachestation.Model_2
}

type SegDiscountCode struct {
	SegmentCode string `json:"SC"`
	*cachestation.DiscountCodeAir
}

/**
折扣查询 输入参数
*/
type DiscountCode_In struct {
	OfficeIDs      []string              `json:"OfficeIDs"`      //注册公司（可传入多个）
	DataSourceCode string                `json:"DataSourceCode"` //数据源编码 (可"") 例如1AYQFDS。（abs上面没有数据源，那就空）
	DiscountCode   string                `json:"DiscountCode"`   //折扣代码(可"")
	Airline        string                `json:"Airline"`        //航司2字代码(可"")
	Flight         []*Point2Point_Flight `json:"Legs"`           //航段信息（传json数据句要认准后面这段json:"Legs",这里才是最真实的）
	Type           string                `json:"Type"`           //机票类型 A(国际机票) B(国内机票)
	ApplyHumen     string                `json:"ApplyHumen"`     //适合人群(可"") A(一般成人) B(学生) C(青年) D(移民) E(劳务) F(海员) G(特殊身份) H(一般儿童) I(移民儿童)
}

//获取航班代理费，返点数据查询的参数-----。在这里查询
//#TODO 袁攀在一起飞新接口封装了这个接口
type Precise_DC_In struct {
	OfficeId   string            `json:"OfficeId"`       //注册公司
	Agency     string            `json:"Agency"`         //代理商
	Airline    string            `json:"PlatingCarrier"` //航司
	Price      int               `json:"Price"`          // 票面价，不含税
	Type       string            `json:"Type"`           //机票类型 A(国际机票) B(国内机票)
	ApplyHumen string            `json:"ApplyHumen"`     //适合人群(可"") A(一般成人) B(学生) C(青年) D(移民) E(劳务) F(海员) G(特殊身份) H(一般儿童) I(移民儿童)
	Segment    []*PreciseSegment `json:"Segment"`        //航班信息 ，支持多行程
}

type PreciseSegment struct {
	Legs []*PreciseLeg `json:"Legs"`
}

type PreciseLeg struct {
	Departure     string `json:"Departure"`     //出发地
	Arrival       string `json:"Arrival"`       //目的地
	DepartureDate string `json:"DepartureDate"` //出发时间
	ArrivalDate   string `json:"ArrivalDate"`   //到达时间
	Airline       string `json:"Airline"`       //航司
	FlightNumber  string `json:"FlightNumber"`  //航班号，必须传入四位
}

var DiscountCodeErrOut []byte



/**
折扣代码的机场处理
dcin 为查询条件
rout 整理好的条件
leg 代表第几段
chan_dc_out 为最终要输出的内容

*/

func DiscountCodeAir_V1(dcin *DiscountCode_In, rout *mysqlop.Routine, leg int, chan_dc_out chan *DiscountCode_Output) {

	defer errorlog.DealRecoverLog()

	var (
		DepartStation string
		DepartCountry string
		ArriveStation string
		ArriveCountry string
		TravelDate    string
		Trip          int
		GoorBack      int
		CrossSeason   string
		DiscountCode  string
		SegmentCode   string
		dc            [2]cachestation.LDestSpriAirL

		map_dsa         cachestation.LDestSpriAirL
		dca_array       []*cachestation.DiscountCodeAir  //数组，找到的符合条件的折扣代码数组
		DestSpriAirL    []string
		DestSpriAirL_V2 []string
		s_today         string
		today           time.Time
		Departuredate   time.Time  //出发时间
		ok              bool
		days            int

		dc_out  *DiscountCode_Output  //最终要输出
		model_2 *cachestation.Model_2    //其中一个数据源配置
		model2s []*cachestation.Model_2  //数据源配置数组  里面装wudy发布的个个数据源
		dc_ds   map[string]bool
		dc_data map[string]struct{} //已经获取得到数据的DiscountCode
	)

	dc_data = make(map[string]struct{}, 10)

	/**
	下面的数据处理完了之后，在defer里面将处理得到的数据，再输出给chan_dc_out <- dc_out
	*/

	defer func() {
		//过滤掉没有使用过的折扣代码
		Len := len(dc_out.Model_3)
		for i := 0; i < Len; {
			if _, ok := dc_data[dc_out.Model_3[i].Model_2.DiscountCode]; !ok {
				Len--
				dc_out.Model_3[i], dc_out.Model_3[Len] = dc_out.Model_3[Len], dc_out.Model_3[i]
			} else {
				i++
			}
		}
		dc_out.Model_3 = dc_out.Model_3[:Len]
		chan_dc_out <- dc_out
	}()


	dc_out = &DiscountCode_Output{}
	if leg == 0 {
		dc_out.Model_3 = make([]*DC_Model_3, 0, 15)
		//dc_out.DataSource = make([]string, 0, 15)
	}

	dc_out.SegDiscountCode = make([]*SegDiscountCode, 0, 150)
	dc_out.ExceptCode = make([]*SegDiscountCode, 0, 50)
	dc_ds = make(map[string]bool)

	DepartStation = rout.DepartStation //出发机场
	ArriveStation = rout.ArriveStation  //到达机场
	TravelDate = rout.TravelDate  //出发时间
	Trip = rout.Trip
	GoorBack = rout.GoorBack
	CrossSeason = rout.CrossSeason
	SegmentCode = strconv.Itoa(leg + 1)

	/**

	缓存中查看是否有这个机场的城市代码，机场代码等
	*/

	if DepartCountry, ok = cachestation.CityCountry[DepartStation]; !ok {

		if county, ok := cachestation.County[DepartStation]; ok {
			if DepartCountry, ok = cachestation.CityCountry[county.City]; !ok {
				return
			}
		} else {
			return
		}
	}

	if ArriveCountry, ok = cachestation.CityCountry[ArriveStation]; !ok {
		if county, ok := cachestation.County[ArriveStation]; ok {
			if ArriveCountry, ok = cachestation.CityCountry[county.City]; !ok {
				return
			}
		} else {
			return
		}
	}

	if county, ok := cachestation.County[DepartStation]; ok {
		DepartStation = county.City
	}

	if county, ok := cachestation.County[ArriveStation]; ok {
		ArriveStation = county.City
	}

	/**
	出发地目的地航司，一些匹配组合
	*/
	DestSpriAirL, DestSpriAirL_V2 = Get_DestSpriAirL(DepartStation, DepartCountry, ArriveStation, ArriveCountry, dcin.Airline)
	s_today = errorlog.Today()
	today, _ = time.Parse("2006-01-02", s_today)
	Departuredate, _ = time.Parse("2006-01-02", TravelDate)
	days = int(Departuredate.Sub(today).Hours() / 24)  //days 代表出发出发日期距离今天多远

	//遍历传入的OfficeIDs。将其中一个个拿出来处理
	for _, OfficeID := range dcin.OfficeIDs {

		if OfficeID != "SysCallDiscountCode" {
			//cachestation.Model_3Lock.RLock()
			model2s, ok = cachestation.Model_3[OfficeID]
			//cachestation.Model_3Lock.RUnlock()
		} else { //专指定折扣代码的暂时用户ID SysCallDiscountCode
			//把专门需要给出的DiscountCode这里处理
			model2s = []*cachestation.Model_2{{DiscountCode: dcin.DiscountCode}}
			ok = true
		}

		if ok {
			for _, model_2 = range model2s {
				if dcin.DataSourceCode != "" &&
					dcin.DataSourceCode != model_2.DataSource &&
					OfficeID != "SysCallDiscountCode" { //非专折扣代码要求
					continue //指定数据源
				}

				DiscountCode = model_2.DiscountCode
				//如果有指定DiscountCode,从这里过滤多余部分.

				if leg == 0 && model_2.DataSource != "" {
					dc_out.Model_3 = append(dc_out.Model_3, &DC_Model_3{User: OfficeID, Model_2: model_2})
				}

				if ok = dc_ds[DiscountCode]; ok { //如果该折扣代码已经加入则不继续加入
					continue
				} else {
					dc_ds[DiscountCode] = true
				}

				done := 0
			Again:
				if done == 0 {
					cachestation.DCAtypeB.M.RLock()
					dc, ok = cachestation.DCAtypeB.CacheDiscountCodeAir[DiscountCode]
					cachestation.DCAtypeB.M.RUnlock()
				} else {
					cachestation.DCAtypeA.M.RLock()
					dc, ok = cachestation.DCAtypeA.CacheDiscountCodeAir[DiscountCode]
					cachestation.DCAtypeA.M.RUnlock()
				}

				//#TODO 折扣代码 4
				if ok {
					if dcin.Type == "A" { //A是国际票条件代码
						map_dsa = dc[0] //.DestSpriAirL
					} else { // B 国内条件代码
						map_dsa = dc[1] //.DestSpriAirL
					}
				} else {
					if done > 0 { //原来这里的代码
						delete(dc_ds, DiscountCode)
						continue
					} else {
						done++
						goto Again
					}
				}

				//这里定义DiscountCode数据优先级控制
				var haveAL map[string]string //没指定航司时,获取到的航司.获取的规则到舱位[string=Airline + Cabin][string=DestSpriAirL为了解决同舱位多价格段]
				if len(DestSpriAirL_V2) > 0 {
					haveAL = make(map[string]string, 300)
				}

				NoALimportant := false //没指定航司的优先重要
				ALimportant := false   //有指定航司的优先重要

				for _, dsa := range DestSpriAirL {

					//如果航司没数据的话，则跳过
					if NoALimportant && (ALimportant || dsa[7] == '*') {
						continue
					}

					//map_dsa.M.RLock()

					dca_array, ok = map_dsa.DestSpriAirL[dsa]

					//map_dsa.M.RUnlock()

					if ok {

						/**
						根据整理后的数据，一步步从内存中将数据进行比较。如果都符合的情况下才能进入下一步
						如dcin.ApplyHumen==""则代表无显示运用人群，如dcin.CountryTeam 和 dcin.Dest
						下面其实就是根据输入条件，再在折扣代码详情中找出符合查询的情况，将符合的哪些拉出来

						*/
						//#TODO 316行升哥可能判断错了，如果dca.CountryTeam应改为dca.DestCounTeam

						for _, dca := range dca_array {

							if dca.Trip == Trip &&
								dca.GoorBack == GoorBack &&
								dca.AheadDays <= days &&
								dca.ReserveFirstDate <= s_today &&
								dca.ReserveLastDate >= s_today &&
								dca.TravelFirstDate <= TravelDate &&
								dca.TravelLastDate >= TravelDate &&
								(CrossSeason == "A" || dca.CrossSeason == CrossSeason) &&
								(dcin.ApplyHumen == "" || dcin.ApplyHumen == dca.ApplyHumen) &&
								(dca.CountryTeam == "" || !strings.Contains(dca.CountryTeam, DepartCountry)) &&
								(dca.CountryTeam == "" || !strings.Contains(dca.DestCounTeam, ArriveCountry)) {

								if done == 0 {
									dc_out.SegDiscountCode = append(dc_out.SegDiscountCode, &SegDiscountCode{SegmentCode, dca})
								} else {
									dc_out.ExceptCode = append(dc_out.ExceptCode, &SegDiscountCode{SegmentCode, dca})
								}

								NoALimportant = true
								if dsa[7] != '*' {
									ALimportant = true
								}
								dc_data[dca.DiscountCode] = struct{}{}
							}
						}
					}
				}

				if dcin.Airline != "" {
					continue
				}

				if done == 0 {

					//B类折扣代码
					cachestation.DCAtypeB_Airline.M.RLock()
					dc, ok = cachestation.DCAtypeB_Airline.CacheDiscountCodeAir[DiscountCode]
					cachestation.DCAtypeB_Airline.M.RUnlock()
				} else {
					//A类折扣代码
					cachestation.DCAtypeA_Airline.M.RLock()
					dc, ok = cachestation.DCAtypeA_Airline.CacheDiscountCodeAir[DiscountCode]
					cachestation.DCAtypeA_Airline.M.RUnlock()
				}

				if ok {
					if dcin.Type == "A" { //国际
						map_dsa = dc[0] //.DestSpriAirL
					} else { //B 国内
						map_dsa = dc[1] //.DestSpriAirL
					}
				} else {
					continue
				}

				for _, dsa := range DestSpriAirL_V2 {

					/**
					先通过路线查找是否有对应的折扣数据数组；
					如果有，再一个个拿出
					*/

					//map_dsa.M.RLock()
					dca_array, ok = map_dsa.DestSpriAirL[dsa]
					//map_dsa.M.RUnlock()

					if ok {

						for _, dca := range dca_array {
							var suffix = "B"
							if done > 0 {
								suffix = "A"
							}
							tmp_dsal, ok_hs := haveAL[dca.Airline+dca.Berth+suffix]
							if ok_hs && tmp_dsal != dsa {
								continue
							}

							if dca.Trip == Trip &&
								dca.GoorBack == GoorBack &&
								dca.AheadDays <= days &&
								dca.ReserveFirstDate <= s_today &&
								dca.ReserveLastDate >= s_today &&
								dca.TravelFirstDate <= TravelDate &&
								dca.TravelLastDate >= TravelDate &&
								(CrossSeason == "A" || dca.CrossSeason == CrossSeason) &&
								(dcin.ApplyHumen == "" || dcin.ApplyHumen == dca.ApplyHumen) &&
								(dca.CountryTeam == "" || !strings.Contains(dca.CountryTeam, DepartCountry)) &&
								(dca.CountryTeam == "" || !strings.Contains(dca.DestCounTeam, ArriveCountry)) {

									/**
									done == 0 代表   将找到的适合的折扣代码加进去
									非0的话，则将其加入不适合的折扣代码。也就是条件

									*/
								if done == 0 {
									dc_out.SegDiscountCode = append(dc_out.SegDiscountCode, &SegDiscountCode{SegmentCode, dca})
								} else {
									dc_out.ExceptCode = append(dc_out.ExceptCode, &SegDiscountCode{SegmentCode, dca})
								}
								if !ok_hs {
									haveAL[dca.Airline+dca.Berth+suffix] = dsa
								}
								dc_data[dca.DiscountCode] = struct{}{}
							}
						}
					}
				}
				//	goto Again
			}
		}
	}







	}

//折扣代码查询（其实都是在本地上面拿。。如果没有的话，则直接返回空。。。很重要！！！！！）
//#TODO 0814返回的字段EF小数点后很多位，需要修改cachestation.DiscountCodeAir.EncourageFee  也就是后返费的百分比 0.899999其实录入数据是0.9
//数据库保存的格式是float(5,1);查了以后数据都是0.9也是没问题的。 。应该是在返回出去的数据上面有问题
func WAPI_DiscountCodeAir_V1(w http.ResponseWriter, r *http.Request) {
	defer errorlog.DealRecoverLog()
	r.ParseForm()
	result, _ := ioutil.ReadAll(r.Body)
	fmt.Println("调用 WAPI_DiscountCodeAir_V1 参数:", string(result))
	/**
		这个就是输入的条件。准备要用来获取数据的
	type DiscountCode_In struct {
		OfficeIDs      []string              `json:"OfficeIDs"`      //注册公司
		DataSourceCode string                `json:"DataSourceCode"` //数据源编码 (可"") 例如1AYQFDS。（abs上面没有数据源，那就空）
		DiscountCode   string                `json:"DiscountCode"`   //折扣代码(可"")
		Airline        string                `json:"Airline"`        //航司2字代码(可"")
		Flight         []*Point2Point_Flight `json:"Legs"`           //航段信息（传json数据句要认准后面这段json:"Legs",这里才是最真实的）
		Type           string                `json:"Type"`           //机票类型 A(国际机票) B(国内机票)
		ApplyHumen     string                `json:"ApplyHumen"`     //适合人群(可"") A(一般成人) B(学生) C(青年) D(移民) E(劳务) F(海员) G(特殊身份) H(一般儿童) I(移民儿童)
	}
	*/

	var (
		dc_out      *DiscountCode_Output //最终输出去的数据。
		chan_dc_out []chan *DiscountCode_Output  //这里是将查到的数据进行集合
		dc_out_json []byte   //最终要专程二进制的数据出去
		dcin        DiscountCode_In  //查询参数
		lenFlight   int   //总共有几段行程
		rout        []*mysqlop.Routine  //通过输入行程，进行转化后的数据
	)

	defer func() {

		//#TODO 这里就是要返回出去的数据。。。这里的dc_out非常重要
		if dc_out != nil && len(dc_out.SegDiscountCode) > 0 {
			//#TODO 升哥这里进行了压缩。而外面的人要使用需要对其重新解压拿出来。 为了方便调试，将其注释，改为不解压发送出去
			dc_out_json = errorlog.Make_JSON_GZip(dc_out)
		}

		if dc_out_json == nil {
			fmt.Fprint(w, bytes.NewBuffer(DiscountCodeErrOut))
		} else {

			//这里就是将正确的结果返回出去了
			fmt.Fprint(w, bytes.NewBuffer(dc_out_json))
		}
	}()


	if err := json.Unmarshal(result, &dcin); err != nil {
		errorlog.WriteErrorLog("WAPI_DiscountCodeAir_V1 (1): " + string(result))
		return
	}

	//判断有多少个航段
	lenFlight = len(dcin.Flight)
	if lenFlight == 0 || lenFlight > 6 {

		errorlog.WriteErrorLog("WAPI_DiscountCodeAir_V1 (2): Len(Flight)=" + strconv.Itoa(lenFlight))
		return
	}

	chan_dc_out = make([]chan *DiscountCode_Output, lenFlight)

	rout = FlightLegs2Routine(dcin.Flight) //rout是真实取获取的行段

	//下面这句的作用是,特别的返回需要指定的代码,又怕促销时每有OfficeID指定
	/**
	如果查询时传入了折扣代码，则在OfficeIDs这里加入一个"SysCallDiscountCode"，以此来判断是有传入折扣代码的
	*/
	if dcin.DiscountCode != "" {
		dcin.OfficeIDs = append(dcin.OfficeIDs, "SysCallDiscountCode")
	}

	//将查询的行程放在一个数组里面，接着一个个拿出来去查询
	for i, subrout := range rout {
		chan_dc_out[i] = make(chan *DiscountCode_Output, 1)

		/**
		折扣代码SegDiscountCode。查询的结果都是放到chan_dc_out[i] 这里面来了。接着在下面对chan_dc_out[i] 再次进行遍历，然后再处理
		*/

		go DiscountCodeAir_V1(&dcin, subrout, i, chan_dc_out[i])
	}

	for i := range chan_dc_out {

		//将每一段的chan_dc_out[ℹ️]拿出来
		dc_back := <-chan_dc_out[i]

		/**
		如果i==0 则代表其实就是第一段，则直接就像数据拿了。
		如果不为1，则将每个再逐步插入
		*/

		if i == 0 {
			dc_out = dc_back
		} else {

			//#TODO 折扣代码SegDiscountCode 1。出问题的都是在SegDiscountCode这个字段里面，因此我们可以往上查
			dc_out.SegDiscountCode = append(dc_out.SegDiscountCode, dc_back.SegDiscountCode...)
			dc_out.ExceptCode = append(dc_out.ExceptCode, dc_back.ExceptCode...)
		}
	}
}

//折扣代码的精准获取
func DCA_Precise_V1(dcin *Precise_DC_In, rout *mysqlop.Routine, leg int, chan_dc_out chan *DiscountCode_Output) {

	defer errorlog.DealRecoverLog()

	var (
		DepartStation string
		DepartCountry string
		ArriveStation string
		ArriveCountry string
		TravelDate    string
		Trip          int
		GoorBack      int
		CrossSeason   string
		DiscountCode  string
		SegmentCode   string
		dc            [2]cachestation.LDestSpriAirL
		map_dsa       cachestation.LDestSpriAirL
		dca_array     []*cachestation.DiscountCodeAir
		DestSpriAirL  []string
		s_today       string
		today         time.Time
		Departuredate time.Time
		ok            bool
		days          int

		dc_out  *DiscountCode_Output
		model_2 *cachestation.Model_2
		model2s []*cachestation.Model_2

		low_price  int = 2000000
		tmp_price  int
		dca_return *cachestation.DiscountCodeAir
		m2_return  *cachestation.Model_2
	)

	defer func() {
		chan_dc_out <- dc_out
	}()

	dc_out = &DiscountCode_Output{}
	if leg == 0 {
		dc_out.Model_3 = make([]*DC_Model_3, 0, 5)
	}
	dc_out.SegDiscountCode = make([]*SegDiscountCode, 0, 5)
	dc_out.ExceptCode = make([]*SegDiscountCode, 0, 5)

	DepartStation = rout.DepartStation
	ArriveStation = rout.ArriveStation
	TravelDate = rout.TravelDate
	Trip = rout.Trip
	GoorBack = rout.GoorBack
	CrossSeason = rout.CrossSeason
	OfficeID := dcin.OfficeId
	price := dcin.Price
	SegmentCode = strconv.Itoa(leg + 1)

	if DepartCountry, ok = cachestation.CityCountry[DepartStation]; !ok {
		if county, ok := cachestation.County[DepartStation]; ok {
			if DepartCountry, ok = cachestation.CityCountry[county.City]; !ok {
				return
			}
		} else {
			return
		}
	}

	if ArriveCountry, ok = cachestation.CityCountry[ArriveStation]; !ok {
		if county, ok := cachestation.County[ArriveStation]; ok {
			if ArriveCountry, ok = cachestation.CityCountry[county.City]; !ok {
				return
			}
		} else {
			return
		}
	}

	if county, ok := cachestation.County[DepartStation]; ok {
		DepartStation = county.City
	}

	if county, ok := cachestation.County[ArriveStation]; ok {
		ArriveStation = county.City
	}

	DestSpriAirL, _ = Get_DestSpriAirL(DepartStation, DepartCountry, ArriveStation, ArriveCountry, dcin.Airline)

	s_today = errorlog.Today()
	today, _ = time.Parse("2006-01-02", s_today)
	Departuredate, _ = time.Parse("2006-01-02", TravelDate)
	days = int(Departuredate.Sub(today).Hours() / 24)
	var flightNumber []string
	for _, tmpLeg := range dcin.Segment[leg].Legs {
		flightNumber = append(flightNumber, tmpLeg.Airline+tmpLeg.FlightNumber)
	}

	checkFN := func(fns string) bool {
		for _, fn := range flightNumber {
			if strings.Contains(fns, fn) {
				return true
			}
		}
		return false
	}

	//cachestation.Model_3Lock.RLock()
	model2s, ok = cachestation.Model_3[OfficeID]
	//cachestation.Model_3Lock.RUnlock()

	if !ok {
		return
	}

	for _, model_2 = range model2s {
		if dcin.Agency != model_2.DataSource {
			continue
		}

		DiscountCode = model_2.DiscountCode

		cachestation.DCAtypeB.M.RLock()
		dc, ok = cachestation.DCAtypeB.CacheDiscountCodeAir[DiscountCode]
		cachestation.DCAtypeB.M.RUnlock()

		if ok {
			if dcin.Type == "A" { //A是国际票条件代码
				map_dsa = dc[0] //国际票条件代码
			} else { // B 国内条件代码
				map_dsa = dc[1] //.DestSpriAirL
			}
		} else {
			continue
		}

		dc_lowprice := 2000000 //每一折扣代码进入计算一次

		for _, dsa := range DestSpriAirL {

			//map_dsa.M.RLock()
			dca_array, ok = map_dsa.DestSpriAirL[dsa]
			//map_dsa.M.RUnlock()

			if ok {
				for _, dca := range dca_array {
					if dca.Trip == Trip &&
						dca.GoorBack == GoorBack &&
						dca.AheadDays <= days &&
						dca.ReserveFirstDate <= s_today &&
						dca.ReserveLastDate >= s_today &&
						dca.TravelFirstDate <= TravelDate &&
						dca.TravelLastDate >= TravelDate &&
						(CrossSeason == "A" || dca.CrossSeason == CrossSeason) &&
						(dca.ApplyHumen == "" || dcin.ApplyHumen == dca.ApplyHumen) &&
						(dca.CountryTeam == "" || !strings.Contains(dca.CountryTeam, DepartCountry)) &&
						(dca.CountryTeam == "" || !strings.Contains(dca.DestCounTeam, ArriveCountry)) &&
						(dca.UseFlightNumber == "" || checkFN(dca.UseFlightNumber)) &&
						(dca.NonUseFlightNumber == "" || !checkFN(dca.NonUseFlightNumber)) {
						//这里没有处理适合舱位,但又没办法
						tmp_price = price - int(float64(price)*(dca.AgencyFee+dca.EncourageFee)/100) + dca.TicketFee
						if low_price > tmp_price {
							low_price = tmp_price
							dca_return = dca
							m2_return = model_2
						}

						dc_lowprice = tmp_price
					}
				}
			}

			if dc_lowprice != 2000000 {
				break
			}
		}
	}
	if leg == 0 {
		dc_out.Model_3 = append(dc_out.Model_3, &DC_Model_3{User: OfficeID, Model_2: m2_return})
	}
	dc_out.SegDiscountCode = append(dc_out.SegDiscountCode, &SegDiscountCode{SegmentCode, dca_return})
}


/**
WAPI.DCAPrecise（这个接口和袁攀开发的接口一样。航班代理费，返点数据查询。应该是这个了） 航班代理费，返点数据查询
精准折扣查询
*/
func WAPI_DCA_Precise_V1(w http.ResponseWriter, r *http.Request) {
	defer errorlog.DealRecoverLog()

	r.ParseForm()
	result, _ := ioutil.ReadAll(r.Body)

	var (
		dc_out      *DiscountCode_Output
		chan_dc_out []chan *DiscountCode_Output
		dc_out_json []byte
		dcin        Precise_DC_In
		lenFlight   int
		Flight      []*Point2Point_Flight
		rout        []*mysqlop.Routine
	)

	//反正最终要返回的东西都是在这里defer 函数返回出去
	defer func() {
		if dc_out != nil {
			dc_out_json = errorlog.Make_JSON_GZip(dc_out)
		}

		if dc_out_json == nil {
			fmt.Fprint(w, bytes.NewBuffer(DiscountCodeErrOut))
		} else {
			fmt.Fprint(w, bytes.NewBuffer(dc_out_json))
		}
	}()

	//数据从byte转model
	if err := json.Unmarshal(result, &dcin); err != nil {
		errorlog.WriteErrorLog("WAPI_DCA_Precise_V1 (1): " + string(result))
		return
	}

	//遍历多个行程
	for _, seg := range dcin.Segment {

		//表示有多少个航段
		tmpLen := len(seg.Legs)

		Flight = append(Flight, &Point2Point_Flight{
			DepartStation: seg.Legs[0].Departure,      //出发地
			ArriveStation: seg.Legs[tmpLen-1].Arrival, //最后
			TravelDate:    seg.Legs[0].DepartureDate[:10],
		})
	}

	lenFlight = len(Flight)
	if lenFlight == 0 || lenFlight > 5 {
		errorlog.WriteErrorLog("WAPI_DCA_Precise_V1 (2): Len(Flight)=" + strconv.Itoa(lenFlight))
		return
	}

	chan_dc_out = make([]chan *DiscountCode_Output, lenFlight)

	rout = FlightLegs2Routine(Flight) //rout是真实取获取的行段

	for i, subrout := range rout {
		chan_dc_out[i] = make(chan *DiscountCode_Output, 1)
		go DCA_Precise_V1(&dcin, subrout, i, chan_dc_out[i])
	}

	for i := range chan_dc_out {
		dc_back := <-chan_dc_out[i]

		if i == 0 {
			dc_out = dc_back
		} else {
			dc_out.SegDiscountCode = append(dc_out.SegDiscountCode, dc_back.SegDiscountCode...)
			dc_out.ExceptCode = append(dc_out.ExceptCode, dc_back.ExceptCode...)
		}
	}
}
