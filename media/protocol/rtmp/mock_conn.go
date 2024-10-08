// Code generated by MockGen. DO NOT EDIT.
// Source: conn.go

// Package rtmp is a generated GoMock package.
package rtmp

import (
	reflect "reflect"

	av "github.com/bugVanisher/streamer/media/av"
	flvio "github.com/bugVanisher/streamer/media/container/flv/flvio"
	common "github.com/bugVanisher/streamer/media/protocol/common"
	gomock "github.com/golang/mock/gomock"
)

// MockConn is a mock of Conn interface.
type MockConn struct {
	ctrl     *gomock.Controller
	recorder *MockConnMockRecorder
}

// MockConnMockRecorder is the mock recorder for MockConn.
type MockConnMockRecorder struct {
	mock *MockConn
}

// NewMockConn creates a new mock instance.
func NewMockConn(ctrl *gomock.Controller) *MockConn {
	mock := &MockConn{ctrl: ctrl}
	mock.recorder = &MockConnMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockConn) EXPECT() *MockConnMockRecorder {
	return m.recorder
}

// Close mocks base method.
func (m *MockConn) Close() error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Close")
	ret0, _ := ret[0].(error)
	return ret0
}

// Close indicates an expected call of Close.
func (mr *MockConnMockRecorder) Close() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Close", reflect.TypeOf((*MockConn)(nil).Close))
}

// ConnectPlay mocks base method.
func (m *MockConn) ConnectPlay() error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "ConnectPlay")
	ret0, _ := ret[0].(error)
	return ret0
}

// ConnectPlay indicates an expected call of ConnectPlay.
func (mr *MockConnMockRecorder) ConnectPlay() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "ConnectPlay", reflect.TypeOf((*MockConn)(nil).ConnectPlay))
}

// ConnectPublish mocks base method.
func (m *MockConn) ConnectPublish() error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "ConnectPublish")
	ret0, _ := ret[0].(error)
	return ret0
}

// ConnectPublish indicates an expected call of ConnectPublish.
func (mr *MockConnMockRecorder) ConnectPublish() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "ConnectPublish", reflect.TypeOf((*MockConn)(nil).ConnectPublish))
}

// HandshakeClient mocks base method.
func (m *MockConn) HandshakeClient() error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "HandshakeClient")
	ret0, _ := ret[0].(error)
	return ret0
}

// HandshakeClient indicates an expected call of HandshakeClient.
func (mr *MockConnMockRecorder) HandshakeClient() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "HandshakeClient", reflect.TypeOf((*MockConn)(nil).HandshakeClient))
}

// HandshakeServer mocks base method.
func (m *MockConn) HandshakeServer() error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "HandshakeServer")
	ret0, _ := ret[0].(error)
	return ret0
}

// HandshakeServer indicates an expected call of HandshakeServer.
func (mr *MockConnMockRecorder) HandshakeServer() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "HandshakeServer", reflect.TypeOf((*MockConn)(nil).HandshakeServer))
}

// Headers mocks base method.
func (m *MockConn) Headers() ([]av.CodecData, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Headers")
	ret0, _ := ret[0].([]av.CodecData)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// Headers indicates an expected call of Headers.
func (mr *MockConnMockRecorder) Headers() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Headers", reflect.TypeOf((*MockConn)(nil).Headers))
}

// Info mocks base method.
func (m *MockConn) Info() common.Info {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Info")
	ret0, _ := ret[0].(common.Info)
	return ret0
}

// Info indicates an expected call of Info.
func (mr *MockConnMockRecorder) Info() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Info", reflect.TypeOf((*MockConn)(nil).Info))
}

// OnStatus mocks base method.
func (m *MockConn) OnStatus(msg flvio.AMFMap) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "OnStatus", msg)
	ret0, _ := ret[0].(error)
	return ret0
}

// OnStatus indicates an expected call of OnStatus.
func (mr *MockConnMockRecorder) OnStatus(msg interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "OnStatus", reflect.TypeOf((*MockConn)(nil).OnStatus), msg)
}

// ReadConnect mocks base method.
func (m *MockConn) ReadConnect() error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "ReadConnect")
	ret0, _ := ret[0].(error)
	return ret0
}

// ReadConnect indicates an expected call of ReadConnect.
func (mr *MockConnMockRecorder) ReadConnect() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "ReadConnect", reflect.TypeOf((*MockConn)(nil).ReadConnect))
}

// ReadPacket mocks base method.
func (m *MockConn) ReadPacket() (av.Packet, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "ReadPacket")
	ret0, _ := ret[0].(av.Packet)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// ReadPacket indicates an expected call of ReadPacket.
func (mr *MockConnMockRecorder) ReadPacket() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "ReadPacket", reflect.TypeOf((*MockConn)(nil).ReadPacket))
}

// RemoteAddr mocks base method.
func (m *MockConn) RemoteAddr() string {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "RemoteAddr")
	ret0, _ := ret[0].(string)
	return ret0
}

// RemoteAddr indicates an expected call of RemoteAddr.
func (mr *MockConnMockRecorder) RemoteAddr() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "RemoteAddr", reflect.TypeOf((*MockConn)(nil).RemoteAddr))
}

// VideoResolution mocks base method.
func (m *MockConn) VideoResolution() (uint32, uint32) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "VideoResolution")
	ret0, _ := ret[0].(uint32)
	ret1, _ := ret[1].(uint32)
	return ret0, ret1
}

// VideoResolution indicates an expected call of VideoResolution.
func (mr *MockConnMockRecorder) VideoResolution() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "VideoResolution", reflect.TypeOf((*MockConn)(nil).VideoResolution))
}

// WriteHeader mocks base method.
func (m *MockConn) WriteHeader(arg0 []av.CodecData) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "WriteHeader", arg0)
	ret0, _ := ret[0].(error)
	return ret0
}

// WriteHeader indicates an expected call of WriteHeader.
func (mr *MockConnMockRecorder) WriteHeader(arg0 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "WriteHeader", reflect.TypeOf((*MockConn)(nil).WriteHeader), arg0)
}

// WritePacket mocks base method.
func (m *MockConn) WritePacket(arg0 av.Packet) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "WritePacket", arg0)
	ret0, _ := ret[0].(error)
	return ret0
}

// WritePacket indicates an expected call of WritePacket.
func (mr *MockConnMockRecorder) WritePacket(arg0 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "WritePacket", reflect.TypeOf((*MockConn)(nil).WritePacket), arg0)
}

// WriteTrailer mocks base method.
func (m *MockConn) WriteTrailer() error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "WriteTrailer")
	ret0, _ := ret[0].(error)
	return ret0
}

// WriteTrailer indicates an expected call of WriteTrailer.
func (mr *MockConnMockRecorder) WriteTrailer() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "WriteTrailer", reflect.TypeOf((*MockConn)(nil).WriteTrailer))
}
