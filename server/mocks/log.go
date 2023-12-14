// Code generated by MockGen. DO NOT EDIT.
// Source: server/log.go
//
// Generated by this command:
//
//      mockgen -source=server/log.go
//
// Package mocks is a generated GoMock package.
package mocks

import (
        reflect "reflect"

        gomock "go.uber.org/mock/gomock"
)

// MockLogger is a mock of Logger interface.
type MockLogger struct {
        ctrl     *gomock.Controller
        recorder *MockLoggerMockRecorder
}

// MockLoggerMockRecorder is the mock recorder for MockLogger.
type MockLoggerMockRecorder struct {
        mock *MockLogger
}

// NewMockLogger creates a new mock instance.
func NewMockLogger(ctrl *gomock.Controller) *MockLogger {
        mock := &MockLogger{ctrl: ctrl}
        mock.recorder = &MockLoggerMockRecorder{mock}
        return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockLogger) EXPECT() *MockLoggerMockRecorder {
        return m.recorder
}

// LogDebug mocks base method.
func (m *MockLogger) LogDebug(msg string, keyValuePairs ...any) {
        m.ctrl.T.Helper()
        varargs := []any{msg}
        for _, a := range keyValuePairs {
                varargs = append(varargs, a)
        }
        m.ctrl.Call(m, "LogDebug", varargs...)
}

// LogDebug indicates an expected call of LogDebug.
func (mr *MockLoggerMockRecorder) LogDebug(msg any, keyValuePairs ...any) *gomock.Call {
        mr.mock.ctrl.T.Helper()
        varargs := append([]any{msg}, keyValuePairs...)
        return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "LogDebug", reflect.TypeOf((*MockLogger)(nil).LogDebug), varargs...)
}

// LogError mocks base method.
func (m *MockLogger) LogError(msg string, keyValuePairs ...any) {
        m.ctrl.T.Helper()
        varargs := []any{msg}
        for _, a := range keyValuePairs {
                varargs = append(varargs, a)
        }
        m.ctrl.Call(m, "LogError", varargs...)
}

// LogError indicates an expected call of LogError.
func (mr *MockLoggerMockRecorder) LogError(msg any, keyValuePairs ...any) *gomock.Call {
        mr.mock.ctrl.T.Helper()
        varargs := append([]any{msg}, keyValuePairs...)
        return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "LogError", reflect.TypeOf((*MockLogger)(nil).LogError), varargs...)
}

// LogInfo mocks base method.
func (m *MockLogger) LogInfo(msg string, keyValuePairs ...any) {
        m.ctrl.T.Helper()
        varargs := []any{msg}
        for _, a := range keyValuePairs {
                varargs = append(varargs, a)
        }
        m.ctrl.Call(m, "LogInfo", varargs...)
}

// LogInfo indicates an expected call of LogInfo.
func (mr *MockLoggerMockRecorder) LogInfo(msg any, keyValuePairs ...any) *gomock.Call {
        mr.mock.ctrl.T.Helper()
        varargs := append([]any{msg}, keyValuePairs...)
        return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "LogInfo", reflect.TypeOf((*MockLogger)(nil).LogInfo), varargs...)
}

// LogWarn mocks base method.
func (m *MockLogger) LogWarn(msg string, keyValuePairs ...any) {
        m.ctrl.T.Helper()
        varargs := []any{msg}
        for _, a := range keyValuePairs {
                varargs = append(varargs, a)
        }
        m.ctrl.Call(m, "LogWarn", varargs...)
}

// LogWarn indicates an expected call of LogWarn.
func (mr *MockLoggerMockRecorder) LogWarn(msg any, keyValuePairs ...any) *gomock.Call {
        mr.mock.ctrl.T.Helper()
        varargs := append([]any{msg}, keyValuePairs...)
        return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "LogWarn", reflect.TypeOf((*MockLogger)(nil).LogWarn), varargs...)
}