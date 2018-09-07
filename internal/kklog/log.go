//这个包主要是对日志的使用

package kklog

import (
	"io"
	"os"
	"sync"
	"fmt"
)

type PartFunc func(*Logger)

type Logger struct {
	mu            sync.Mutex
	buf           []byte
	level         Level
	enableColor   bool
	name          string
	parts         []PartFunc
	colorFile     *ColorFile
	output        io.Writer
	currColor     Color
	currLevel     Level
	currText      string
	currCondition bool
}

const lineBuffer = 32

func New(name string) *Logger {
	log := &Logger{

		name:          name,
		output:        os.Stdout,
		level:         Level_Debug,
		buf:           make([]byte, 0, lineBuffer),
		currCondition: true,
	}

	log.SetParts(LogPart_Level, LogPart_Name, LogPart_Time)

	add(log)

	return log
}

//设置输出
func (self *Logger) SetOutput(writer io.Writer) {
	self.output = writer
}

//是否输出颜色
func (self *Logger) EnableColor(v bool) {
	self.enableColor = v
}

func (self *Logger) SetParts(f ...PartFunc) {

	self.parts = []PartFunc{logPart_ColorBegin}
	self.parts = append(self.parts, f...)
	self.parts = append(self.parts, logPart_Text, logPart_ColorEnd, logPart_Line)
}

func (self *Logger) WriteRawString(s string) {

	self.buf = append(self.buf, s...)

}

func (self *Logger) WriteRawByte(b byte) {

	self.buf = append(self.buf, b)

}
func (self *Logger) Name() string {

	return self.name
}

func (self *Logger)selectColorByLevel()  {


	if levelColor := colorFromLevel(self.currLevel); levelColor != NoColor {
		self.currColor = levelColor
	}
}

func (self *Logger)selectColorByText()  {

	if self.enableColor && self.colorFile != nil && self.currColor == NoColor {
		self.currColor = self.colorFile.ColorFromText(self.currText)
	}

	return


}

func (self *Logger) Log(level Level, text string) {

	self.mu.Lock()
	defer self.mu.Unlock()

	self.currLevel = level
	self.currText = text

	defer self.resetState()

	if self.currLevel < self.level || !self.currCondition {
		return
	}

	self.selectColorByText()

	self.selectColorByLevel()

	self.buf = self.buf[:0]

	for _,p := range self.parts{
		p(self)
	}

	self.output.Write(self.buf)

}

func (self *Logger) Condition(value bool) *Logger {

	self.mu.Lock()
	self.currCondition = value
	self.mu.Unlock()
	return self
}

func (self *Logger) resetState() {
	self.currColor = NoColor
	self.currCondition = true
}



func (self *Logger) SetColor(name string) *Logger {

	self.mu.Lock()
	self.currColor = matchColor(name)
	self.mu.Unlock()
	return self
}

func (self *Logger) Debugf(format string, v ...interface{}) {

	self.Log(Level_Debug,fmt.Sprintf(format,v ...))
}

func (self *Logger) Infof(format string, v ...interface{}) {
	self.Log(Level_Info,fmt.Sprintf(format,v ...))

}

func (self *Logger) Warnf(format string, v ...interface{}) {
	self.Log(Level_Warn, fmt.Sprintf(format, v...))

}

func (self *Logger) Errorf(format string, v ...interface{}) {
	self.Log(Level_Error, fmt.Sprintf(format, v...))

}

func (self *Logger) Debugln(v ...interface{}) {

	self.Log(Level_Debug,fmt.Sprintln(v...))
}

func (self *Logger) Infoln(v ...interface{}) {
	self.Log(Level_Info,fmt.Sprintln(v...))

}

func (self *Logger) Warnln(v ...interface{}) {
	self.Log(Level_Warn,fmt.Sprintln(v...))

}

func (self *Logger) Errorln(v ...interface{}) {
	self.Log(Level_Error,fmt.Sprintln(v...))
}

func (self *Logger)SetLevelByString(level string) {

	self.SetLevel(str2logLever(level))
}

func (self *Logger) SetLevel(lev Level) {
	self.level = lev
}

func (self *Logger) Level() Level {
	return self.level
}

func (self *Logger)SetColorFile (file *ColorFile) {

	self.colorFile = file
}

func (self *Logger)IsDebugEnabled()bool {

	return self.level == Level_Debug
}