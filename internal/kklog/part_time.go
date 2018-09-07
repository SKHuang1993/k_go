
//时间输出的格式。如2018-08-09：12：23：45.989223


package kklog

import "time"

func LogPart_Time(log *Logger)  {
	writeTimePart(log,false)
}


func LogPart_TimeMS(log *Logger)  {
	writeTimePart(log,true)

}

/**
log 日志记录
ms 是否显示微秒  例如10：34：28.2332322
*/

func writeTimePart(log *Logger,ms bool)  {

	now := time.Now()

	year,month,day := now.Date()

	//输出2018
	itoa(log,year,4)
	log.WriteRawByte('/')

	itoa(log,int(month),2)
	log.WriteRawByte('/')

	itoa(log,int(day),2)
	log.WriteRawByte(' ')


	hour,minute,second := now.Clock()

	itoa(log,hour,2)
	log.WriteRawByte(':')


	itoa(log,minute,2)
	log.WriteRawByte(':')

	itoa(log,second,2)

	if ms {

		log.WriteRawByte('.')
		itoa(log,now.Nanosecond()/1e3,6)
	}

	log.WriteRawByte(' ')




}