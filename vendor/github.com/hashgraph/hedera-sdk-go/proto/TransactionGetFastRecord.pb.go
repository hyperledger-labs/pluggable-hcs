// Code generated by protoc-gen-go. DO NOT EDIT.
// source: proto/TransactionGetFastRecord.proto

package proto

import (
	fmt "fmt"
	proto "github.com/golang/protobuf/proto"
	math "math"
)

// Reference imports to suppress errors if they are not otherwise used.
var _ = proto.Marshal
var _ = fmt.Errorf
var _ = math.Inf

// This is a compile-time assertion to ensure that this generated file
// is compatible with the proto package it is being compiled against.
// A compilation error at this line likely means your copy of the
// proto package needs to be updated.
const _ = proto.ProtoPackageIsVersion3 // please upgrade the proto package

// Get the tx record of a transaction, given its transaction ID. Once a transaction reaches consensus, then information about whether it succeeded or failed will be available until the end of the receipt period.  Before and after the receipt period, and for a transaction that was never submitted, the receipt is unknown.  This query is free (the payment field is left empty).
type TransactionGetFastRecordQuery struct {
	Header               *QueryHeader   `protobuf:"bytes,1,opt,name=header,proto3" json:"header,omitempty"`
	TransactionID        *TransactionID `protobuf:"bytes,2,opt,name=transactionID,proto3" json:"transactionID,omitempty"`
	XXX_NoUnkeyedLiteral struct{}       `json:"-"`
	XXX_unrecognized     []byte         `json:"-"`
	XXX_sizecache        int32          `json:"-"`
}

func (m *TransactionGetFastRecordQuery) Reset()         { *m = TransactionGetFastRecordQuery{} }
func (m *TransactionGetFastRecordQuery) String() string { return proto.CompactTextString(m) }
func (*TransactionGetFastRecordQuery) ProtoMessage()    {}
func (*TransactionGetFastRecordQuery) Descriptor() ([]byte, []int) {
	return fileDescriptor_0ca29b47f3e918ab, []int{0}
}

func (m *TransactionGetFastRecordQuery) XXX_Unmarshal(b []byte) error {
	return xxx_messageInfo_TransactionGetFastRecordQuery.Unmarshal(m, b)
}
func (m *TransactionGetFastRecordQuery) XXX_Marshal(b []byte, deterministic bool) ([]byte, error) {
	return xxx_messageInfo_TransactionGetFastRecordQuery.Marshal(b, m, deterministic)
}
func (m *TransactionGetFastRecordQuery) XXX_Merge(src proto.Message) {
	xxx_messageInfo_TransactionGetFastRecordQuery.Merge(m, src)
}
func (m *TransactionGetFastRecordQuery) XXX_Size() int {
	return xxx_messageInfo_TransactionGetFastRecordQuery.Size(m)
}
func (m *TransactionGetFastRecordQuery) XXX_DiscardUnknown() {
	xxx_messageInfo_TransactionGetFastRecordQuery.DiscardUnknown(m)
}

var xxx_messageInfo_TransactionGetFastRecordQuery proto.InternalMessageInfo

func (m *TransactionGetFastRecordQuery) GetHeader() *QueryHeader {
	if m != nil {
		return m.Header
	}
	return nil
}

func (m *TransactionGetFastRecordQuery) GetTransactionID() *TransactionID {
	if m != nil {
		return m.TransactionID
	}
	return nil
}

// Response when the client sends the node TransactionGetFastRecordQuery. If it created a new entity (account, file, or smart contract instance) then one of the three ID fields will be filled in with the ID of the new entity. Sometimes a single transaction will create more than one new entity, such as when a new contract instance is created, and this also creates the new account that it owned by that instance.
type TransactionGetFastRecordResponse struct {
	Header               *ResponseHeader    `protobuf:"bytes,1,opt,name=header,proto3" json:"header,omitempty"`
	TransactionRecord    *TransactionRecord `protobuf:"bytes,2,opt,name=transactionRecord,proto3" json:"transactionRecord,omitempty"`
	XXX_NoUnkeyedLiteral struct{}           `json:"-"`
	XXX_unrecognized     []byte             `json:"-"`
	XXX_sizecache        int32              `json:"-"`
}

func (m *TransactionGetFastRecordResponse) Reset()         { *m = TransactionGetFastRecordResponse{} }
func (m *TransactionGetFastRecordResponse) String() string { return proto.CompactTextString(m) }
func (*TransactionGetFastRecordResponse) ProtoMessage()    {}
func (*TransactionGetFastRecordResponse) Descriptor() ([]byte, []int) {
	return fileDescriptor_0ca29b47f3e918ab, []int{1}
}

func (m *TransactionGetFastRecordResponse) XXX_Unmarshal(b []byte) error {
	return xxx_messageInfo_TransactionGetFastRecordResponse.Unmarshal(m, b)
}
func (m *TransactionGetFastRecordResponse) XXX_Marshal(b []byte, deterministic bool) ([]byte, error) {
	return xxx_messageInfo_TransactionGetFastRecordResponse.Marshal(b, m, deterministic)
}
func (m *TransactionGetFastRecordResponse) XXX_Merge(src proto.Message) {
	xxx_messageInfo_TransactionGetFastRecordResponse.Merge(m, src)
}
func (m *TransactionGetFastRecordResponse) XXX_Size() int {
	return xxx_messageInfo_TransactionGetFastRecordResponse.Size(m)
}
func (m *TransactionGetFastRecordResponse) XXX_DiscardUnknown() {
	xxx_messageInfo_TransactionGetFastRecordResponse.DiscardUnknown(m)
}

var xxx_messageInfo_TransactionGetFastRecordResponse proto.InternalMessageInfo

func (m *TransactionGetFastRecordResponse) GetHeader() *ResponseHeader {
	if m != nil {
		return m.Header
	}
	return nil
}

func (m *TransactionGetFastRecordResponse) GetTransactionRecord() *TransactionRecord {
	if m != nil {
		return m.TransactionRecord
	}
	return nil
}

func init() {
	proto.RegisterType((*TransactionGetFastRecordQuery)(nil), "proto.TransactionGetFastRecordQuery")
	proto.RegisterType((*TransactionGetFastRecordResponse)(nil), "proto.TransactionGetFastRecordResponse")
}

func init() {
	proto.RegisterFile("proto/TransactionGetFastRecord.proto", fileDescriptor_0ca29b47f3e918ab)
}

var fileDescriptor_0ca29b47f3e918ab = []byte{
	// 265 bytes of a gzipped FileDescriptorProto
	0x1f, 0x8b, 0x08, 0x00, 0x00, 0x00, 0x00, 0x00, 0x02, 0xff, 0x74, 0x90, 0xbd, 0x6a, 0xc3, 0x30,
	0x10, 0xc7, 0x51, 0xa1, 0x19, 0x54, 0x3a, 0x54, 0xf4, 0xc3, 0x18, 0x02, 0x21, 0x74, 0x08, 0x05,
	0xcb, 0xd0, 0x6e, 0x1d, 0x43, 0x49, 0xd3, 0xad, 0x35, 0x9e, 0xba, 0x29, 0xf2, 0x61, 0x99, 0x12,
	0x9f, 0xd1, 0x29, 0x43, 0x9e, 0xa0, 0xcf, 0xd0, 0xb7, 0x2d, 0x48, 0x6a, 0xea, 0xc4, 0x78, 0x3a,
	0xb8, 0xff, 0xc7, 0xfd, 0x24, 0x7e, 0xdf, 0x59, 0x74, 0x98, 0x97, 0x56, 0xb5, 0xa4, 0xb4, 0x6b,
	0xb0, 0x7d, 0x05, 0xb7, 0x52, 0xe4, 0x0a, 0xd0, 0x68, 0x2b, 0xe9, 0x65, 0x71, 0xee, 0x47, 0x3a,
	0x1d, 0x98, 0xfb, 0xae, 0xf4, 0x36, 0xc8, 0x4b, 0x45, 0x8d, 0x2e, 0xf7, 0x1d, 0x50, 0xdc, 0xdf,
	0x85, 0xfd, 0xc7, 0x0e, 0xec, 0x7e, 0x0d, 0xaa, 0x02, 0x1b, 0x85, 0x34, 0x08, 0x05, 0x50, 0x87,
	0x2d, 0x41, 0x5f, 0x9b, 0x7f, 0x33, 0x3e, 0x1d, 0xa3, 0xf2, 0x4d, 0xe2, 0x81, 0x4f, 0x8c, 0x4f,
	0x24, 0x6c, 0xc6, 0x16, 0x17, 0x8f, 0x22, 0x24, 0x65, 0xef, 0x4e, 0x11, 0x1d, 0xe2, 0x99, 0x5f,
	0xba, 0xff, 0xb2, 0xb7, 0x97, 0xe4, 0xcc, 0x47, 0xae, 0x63, 0xa4, 0xec, 0x6b, 0xc5, 0xb1, 0x75,
	0xfe, 0xc3, 0xf8, 0x6c, 0x8c, 0xe4, 0x0f, 0x5d, 0x64, 0x27, 0x30, 0x37, 0xb1, 0xf9, 0xf8, 0x6d,
	0x07, 0x9e, 0x15, 0xbf, 0x72, 0xa7, 0xbf, 0x18, 0x99, 0x92, 0x21, 0x53, 0xbc, 0x35, 0x8c, 0x2c,
	0xd7, 0x3c, 0xd5, 0xb8, 0x95, 0x06, 0x2a, 0xb0, 0x4a, 0x1a, 0x45, 0xa6, 0xb6, 0xaa, 0x33, 0xa1,
	0xe2, 0x9d, 0x7d, 0x2e, 0xea, 0xc6, 0x99, 0xdd, 0x46, 0x6a, 0xdc, 0xe6, 0x07, 0x35, 0x0f, 0xf6,
	0x8c, 0xaa, 0xaf, 0xac, 0xc6, 0xdc, 0x7b, 0x37, 0x13, 0x3f, 0x9e, 0x7e, 0x03, 0x00, 0x00, 0xff,
	0xff, 0x58, 0x20, 0x4a, 0xae, 0x11, 0x02, 0x00, 0x00,
}