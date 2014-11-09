package utp

import stdlog "log"

type logger struct{}

var log *logger

func (l *logger) Fatal(v ...interface{}){
  if l == nil {
    return
  }
  stdlog.Fatal(v...)
}

func (l *logger) Fatalf(format string, v ...interface{}){
  if l == nil {
    return
  }
  stdlog.Fatalf(format, v...)
}

func (l *logger) Fatalln(v ...interface{}){
  if l == nil {
    return
  }
  stdlog.Fatalln(v...)
}

func (l *logger) Panic(v ...interface{}){
  if l == nil {
    return
  }
  stdlog.Panic(v...)
}

func (l *logger) Panicf(format string, v ...interface{}){
  if l == nil {
    return
  }
  stdlog.Panicf(format, v...)
}

func (l *logger) Panicln(v ...interface{}){
  if l == nil {
    return
  }
  stdlog.Panicln(v...)
}

func (l *logger) Print(v ...interface{}) {
	if l == nil {
		return
	}
	stdlog.Print(v...)
}

func (l *logger) Printf(format string, v ...interface{}){
  if l == nil {
    return
  }
  stdlog.Printf(format, v...)
}

func (l *logger) Println(v ...interface{}){
  if l == nil {
    return
  }
  stdlog.Println(v...)
}
