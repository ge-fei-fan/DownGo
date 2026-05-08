package logging

import (
	"os"
	"path/filepath"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"example.com/downgo/internal/util"
)

type Logger struct {
	logger  *zap.Logger
	logDir  string
	logFile *os.File
}

func New(baseDir string) (*Logger, error) {
	logDir := filepath.Join(baseDir, "data", "logs")
	if err := util.EnsureDir(logDir); err != nil {
		return nil, err
	}

	file, err := os.OpenFile(filepath.Join(logDir, "downgo.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, err
	}

	encoderConfig := zap.NewProductionEncoderConfig()
	encoderConfig.EncodeTime = zapcore.TimeEncoderOfLayout(time.RFC3339)
	encoderConfig.EncodeLevel = zapcore.CapitalLevelEncoder
	encoderConfig.EncodeCaller = zapcore.ShortCallerEncoder

	core := zapcore.NewCore(
		zapcore.NewConsoleEncoder(encoderConfig),
		zapcore.AddSync(file),
		zap.InfoLevel,
	)

	logger := zap.New(core, zap.AddCaller())
	zap.ReplaceGlobals(logger)

	return &Logger{
		logger:  logger,
		logDir:  logDir,
		logFile: file,
	}, nil
}

func (l *Logger) Logger() *zap.Logger {
	return l.logger
}

func (l *Logger) Dir() string {
	return l.logDir
}

func (l *Logger) Close() error {
	if l == nil {
		return nil
	}
	_ = l.logger.Sync()
	if l.logFile != nil {
		return l.logFile.Close()
	}
	return nil
}
