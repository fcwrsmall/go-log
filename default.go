package log

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"gopkg.in/natefinch/lumberjack.v2"
)

// 自定义的WriteSyncer，每次写入时检查并更新文件名
type timeBasedWriteSyncer struct {
	basePath    string
	current     *lumberjack.Logger
	currentHour int
	serverLog   *lumberjack.Logger
}

func newTimeBasedWriteSyncer(basePath string) *timeBasedWriteSyncer {
	return &timeBasedWriteSyncer{
		basePath:    basePath,
		current:     nil,
		currentHour: -1,
		serverLog: &lumberjack.Logger{
			Filename:   filepath.Join(basePath, "server.log"),
			MaxSize:    1000, // MB
			MaxBackups: 1,    // 只保留当前文件
			MaxAge:     1,    // 保留1小时
			Compress:   false,
		},
	}
}

func (w *timeBasedWriteSyncer) Write(p []byte) (n int, err error) {
	currentTime := time.Now()
	currentHour := currentTime.Hour()
	currentFile := filepath.Join(w.basePath, fmt.Sprintf("server-%s.log", currentTime.Format("2006-01-02-15")))

	// 如果小时变化了，清空server.log
	if w.currentHour != currentHour {
		w.currentHour = currentHour
		// 关闭现有的server.log
		if w.serverLog != nil {
			w.serverLog.Close()
		}

		// 清空server.log文件内容
		serverLogPath := filepath.Join(w.basePath, "server.log")
		if err := os.Truncate(serverLogPath, 0); err != nil {
			// 如果文件不存在，忽略错误
			if !os.IsNotExist(err) {
				fmt.Printf("Warning: failed to truncate server.log: %v\n", err)
			}
		}

		// 重新创建server.log
		w.serverLog = &lumberjack.Logger{
			Filename:   serverLogPath,
			MaxSize:    1000, // MB
			MaxBackups: 1,    // 只保留当前文件
			MaxAge:     1,    // 保留1小时
			Compress:   false,
		}
	}

	// 如果文件名变化了，创建新的logger
	if w.current == nil || w.current.Filename != currentFile {
		w.current = &lumberjack.Logger{
			Filename:   currentFile,
			MaxSize:    1000, // MB
			MaxBackups: 24,   // 保留24小时的日志
			MaxAge:     24,   // 保留24小时
			Compress:   true,
		}
	}

	// 写入带时间戳的日志文件
	if _, err := w.current.Write(p); err != nil {
		return 0, err
	}

	// 同时写入server.log
	return w.serverLog.Write(p)
}

func (w *timeBasedWriteSyncer) Sync() error {
	if w.current != nil {
		w.current.Close()
	}
	if w.serverLog != nil {
		w.serverLog.Close()
	}
	return nil
}

func defaultLog(logPath string, level string) *zap.SugaredLogger {
	var sugar *zap.SugaredLogger
	var logLevel zap.AtomicLevel
	err := logLevel.UnmarshalText([]byte(level))
	if err != nil {
		panic(fmt.Errorf("log Init level error: %v", err))
	}

	// 配置日志文件路径
	// logPath := conf.Str("log.path", "./logs")
	if err := os.MkdirAll(logPath, 0755); err != nil {
		panic(fmt.Errorf("create log directory error: %v", err))
	}

	// 创建基于时间的写入器
	timeBasedSyncer := newTimeBasedWriteSyncer(logPath)

	// 创建编码器配置
	encoderConfig := zap.NewProductionEncoderConfig()
	encoderConfig.EncodeTime = func(t time.Time, encoder zapcore.PrimitiveArrayEncoder) {
		encoder.AppendString(t.Format("2006-01-02 15:04:05.000000"))
	}
	encoderConfig.EncodeCaller = func(caller zapcore.EntryCaller, encoder zapcore.PrimitiveArrayEncoder) {
		index := strings.LastIndex(caller.Function, "/")
		encoder.AppendString(fmt.Sprintf("%s:%d", caller.Function[index+1:], caller.Line))
	}

	// 创建文件输出
	fileEncoder := zapcore.NewJSONEncoder(encoderConfig)
	timeFileCore := zapcore.NewCore(
		fileEncoder,
		zapcore.AddSync(timeBasedSyncer),
		logLevel,
	)

	// 创建控制台输出
	consoleEncoder := zapcore.NewConsoleEncoder(encoderConfig)
	consoleCore := zapcore.NewCore(
		consoleEncoder,
		zapcore.AddSync(os.Stdout),
		logLevel,
	)

	// 合并多个输出
	core := zapcore.NewTee(timeFileCore, consoleCore)

	// 创建logger
	l := zap.New(core, zap.AddCallerSkip(1), zap.AddCaller())
	sugar = l.Sugar()
	return sugar
}
