// Code generated by MockGen. DO NOT EDIT.
// Source: $GOPATH/pkg/mod/github.com/prometheus/prometheus@v0.47.2/storage/interface.go
//
// Generated by this command:
//
//	mockgen -source=$GOPATH/pkg/mod/github.com/prometheus/prometheus@v0.47.2/storage/interface.go -destination=server/mocks/storage.go -exclude_interfaces=Appendable,SampleAndChunkQueryable,Storage,ExemplarStorage,Queryable,Querier,ChunkQuerier,ChunkQueryable,LabelQuerier,ExemplarQueryable,ExemplarQuerier,GetRef,ExemplarAppender,HistogramAppender,MetadataUpdater,SeriesSet,Series,ChunkSeriesSet,ChunkSeries,Labels,SampleIterable,ChunkIterable
//
// Package mock_storage is a generated GoMock package.
package mocks

import (
	reflect "reflect"

	exemplar "github.com/prometheus/prometheus/model/exemplar"
	histogram "github.com/prometheus/prometheus/model/histogram"
	labels "github.com/prometheus/prometheus/model/labels"
	metadata "github.com/prometheus/prometheus/model/metadata"
	storage "github.com/prometheus/prometheus/storage"
	gomock "go.uber.org/mock/gomock"
)

// MockAppender is a mock of Appender interface.
type MockAppender struct {
	ctrl     *gomock.Controller
	recorder *MockAppenderMockRecorder
}

// MockAppenderMockRecorder is the mock recorder for MockAppender.
type MockAppenderMockRecorder struct {
	mock *MockAppender
}

// NewMockAppender creates a new mock instance.
func NewMockAppender(ctrl *gomock.Controller) *MockAppender {
	mock := &MockAppender{ctrl: ctrl}
	mock.recorder = &MockAppenderMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockAppender) EXPECT() *MockAppenderMockRecorder {
	return m.recorder
}

// Append mocks base method.
func (m *MockAppender) Append(ref storage.SeriesRef, l labels.Labels, t int64, v float64) (storage.SeriesRef, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Append", ref, l, t, v)
	ret0, _ := ret[0].(storage.SeriesRef)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// Append indicates an expected call of Append.
func (mr *MockAppenderMockRecorder) Append(ref, l, t, v any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Append", reflect.TypeOf((*MockAppender)(nil).Append), ref, l, t, v)
}

// AppendExemplar mocks base method.
func (m *MockAppender) AppendExemplar(ref storage.SeriesRef, l labels.Labels, e exemplar.Exemplar) (storage.SeriesRef, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "AppendExemplar", ref, l, e)
	ret0, _ := ret[0].(storage.SeriesRef)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// AppendExemplar indicates an expected call of AppendExemplar.
func (mr *MockAppenderMockRecorder) AppendExemplar(ref, l, e any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "AppendExemplar", reflect.TypeOf((*MockAppender)(nil).AppendExemplar), ref, l, e)
}

// AppendHistogram mocks base method.
func (m *MockAppender) AppendHistogram(ref storage.SeriesRef, l labels.Labels, t int64, h *histogram.Histogram, fh *histogram.FloatHistogram) (storage.SeriesRef, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "AppendHistogram", ref, l, t, h, fh)
	ret0, _ := ret[0].(storage.SeriesRef)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// AppendHistogram indicates an expected call of AppendHistogram.
func (mr *MockAppenderMockRecorder) AppendHistogram(ref, l, t, h, fh any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "AppendHistogram", reflect.TypeOf((*MockAppender)(nil).AppendHistogram), ref, l, t, h, fh)
}

// Commit mocks base method.
func (m *MockAppender) Commit() error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Commit")
	ret0, _ := ret[0].(error)
	return ret0
}

// Commit indicates an expected call of Commit.
func (mr *MockAppenderMockRecorder) Commit() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Commit", reflect.TypeOf((*MockAppender)(nil).Commit))
}

// Rollback mocks base method.
func (m *MockAppender) Rollback() error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Rollback")
	ret0, _ := ret[0].(error)
	return ret0
}

// Rollback indicates an expected call of Rollback.
func (mr *MockAppenderMockRecorder) Rollback() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Rollback", reflect.TypeOf((*MockAppender)(nil).Rollback))
}

// UpdateMetadata mocks base method.
func (m_2 *MockAppender) UpdateMetadata(ref storage.SeriesRef, l labels.Labels, m metadata.Metadata) (storage.SeriesRef, error) {
	m_2.ctrl.T.Helper()
	ret := m_2.ctrl.Call(m_2, "UpdateMetadata", ref, l, m)
	ret0, _ := ret[0].(storage.SeriesRef)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// UpdateMetadata indicates an expected call of UpdateMetadata.
func (mr *MockAppenderMockRecorder) UpdateMetadata(ref, l, m any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "UpdateMetadata", reflect.TypeOf((*MockAppender)(nil).UpdateMetadata), ref, l, m)
}
