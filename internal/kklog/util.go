package kklog

/**
log 日志组件
i 进行处理的数据
wid 最多几位
*/

func itoa(log *Logger,i int,wid int)  {

	var u = uint(i)
	if u == 0 && wid <=1{
		log.WriteRawByte('0')
		return
	}

	var b [32]byte
	bp := len(b)

	for; u>0 || wid>0 ;u /=10{
		bp --
		wid --
		b[bp] = byte(u%10)+'0'
	}

	log.buf = append(log.buf,b[bp:]...)

}