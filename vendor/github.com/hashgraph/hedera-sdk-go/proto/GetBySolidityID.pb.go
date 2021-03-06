// Code generated by protoc-gen-go. DO NOT EDIT.
// source: proto/GetBySolidityID.proto

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

// Get the IDs in the format used by transactions, given the ID in the format used by Solidity. If the Solidity ID is for a smart contract instance, then both the ContractID and associated AccountID will be returned.
type GetBySolidityIDQuery struct {
	Header               *QueryHeader `protobuf:"bytes,1,opt,name=header,proto3" json:"header,omitempty"`
	SolidityID           string       `protobuf:"bytes,2,opt,name=solidityID,proto3" json:"solidityID,omitempty"`
	XXX_NoUnkeyedLiteral struct{}     `json:"-"`
	XXX_unrecognized     []byte       `json:"-"`
	XXX_sizecache        int32        `json:"-"`
}

func (m *GetBySolidityIDQuery) Reset()         { *m = GetBySolidityIDQuery{} }
func (m *GetBySolidityIDQuery) String() string { return proto.CompactTextString(m) }
func (*GetBySolidityIDQuery) ProtoMessage()    {}
func (*GetBySolidityIDQuery) Descriptor() ([]byte, []int) {
	return fileDescriptor_18620922c5bc1f51, []int{0}
}

func (m *GetBySolidityIDQuery) XXX_Unmarshal(b []byte) error {
	return xxx_messageInfo_GetBySolidityIDQuery.Unmarshal(m, b)
}
func (m *GetBySolidityIDQuery) XXX_Marshal(b []byte, deterministic bool) ([]byte, error) {
	return xxx_messageInfo_GetBySolidityIDQuery.Marshal(b, m, deterministic)
}
func (m *GetBySolidityIDQuery) XXX_Merge(src proto.Message) {
	xxx_messageInfo_GetBySolidityIDQuery.Merge(m, src)
}
func (m *GetBySolidityIDQuery) XXX_Size() int {
	return xxx_messageInfo_GetBySolidityIDQuery.Size(m)
}
func (m *GetBySolidityIDQuery) XXX_DiscardUnknown() {
	xxx_messageInfo_GetBySolidityIDQuery.DiscardUnknown(m)
}

var xxx_messageInfo_GetBySolidityIDQuery proto.InternalMessageInfo

func (m *GetBySolidityIDQuery) GetHeader() *QueryHeader {
	if m != nil {
		return m.Header
	}
	return nil
}

func (m *GetBySolidityIDQuery) GetSolidityID() string {
	if m != nil {
		return m.SolidityID
	}
	return ""
}

// Response when the client sends the node GetBySolidityIDQuery
type GetBySolidityIDResponse struct {
	Header               *ResponseHeader `protobuf:"bytes,1,opt,name=header,proto3" json:"header,omitempty"`
	AccountID            *AccountID      `protobuf:"bytes,2,opt,name=accountID,proto3" json:"accountID,omitempty"`
	FileID               *FileID         `protobuf:"bytes,3,opt,name=fileID,proto3" json:"fileID,omitempty"`
	ContractID           *ContractID     `protobuf:"bytes,4,opt,name=contractID,proto3" json:"contractID,omitempty"`
	XXX_NoUnkeyedLiteral struct{}        `json:"-"`
	XXX_unrecognized     []byte          `json:"-"`
	XXX_sizecache        int32           `json:"-"`
}

func (m *GetBySolidityIDResponse) Reset()         { *m = GetBySolidityIDResponse{} }
func (m *GetBySolidityIDResponse) String() string { return proto.CompactTextString(m) }
func (*GetBySolidityIDResponse) ProtoMessage()    {}
func (*GetBySolidityIDResponse) Descriptor() ([]byte, []int) {
	return fileDescriptor_18620922c5bc1f51, []int{1}
}

func (m *GetBySolidityIDResponse) XXX_Unmarshal(b []byte) error {
	return xxx_messageInfo_GetBySolidityIDResponse.Unmarshal(m, b)
}
func (m *GetBySolidityIDResponse) XXX_Marshal(b []byte, deterministic bool) ([]byte, error) {
	return xxx_messageInfo_GetBySolidityIDResponse.Marshal(b, m, deterministic)
}
func (m *GetBySolidityIDResponse) XXX_Merge(src proto.Message) {
	xxx_messageInfo_GetBySolidityIDResponse.Merge(m, src)
}
func (m *GetBySolidityIDResponse) XXX_Size() int {
	return xxx_messageInfo_GetBySolidityIDResponse.Size(m)
}
func (m *GetBySolidityIDResponse) XXX_DiscardUnknown() {
	xxx_messageInfo_GetBySolidityIDResponse.DiscardUnknown(m)
}

var xxx_messageInfo_GetBySolidityIDResponse proto.InternalMessageInfo

func (m *GetBySolidityIDResponse) GetHeader() *ResponseHeader {
	if m != nil {
		return m.Header
	}
	return nil
}

func (m *GetBySolidityIDResponse) GetAccountID() *AccountID {
	if m != nil {
		return m.AccountID
	}
	return nil
}

func (m *GetBySolidityIDResponse) GetFileID() *FileID {
	if m != nil {
		return m.FileID
	}
	return nil
}

func (m *GetBySolidityIDResponse) GetContractID() *ContractID {
	if m != nil {
		return m.ContractID
	}
	return nil
}

func init() {
	proto.RegisterType((*GetBySolidityIDQuery)(nil), "proto.GetBySolidityIDQuery")
	proto.RegisterType((*GetBySolidityIDResponse)(nil), "proto.GetBySolidityIDResponse")
}

func init() {
	proto.RegisterFile("proto/GetBySolidityID.proto", fileDescriptor_18620922c5bc1f51)
}

var fileDescriptor_18620922c5bc1f51 = []byte{
	// 286 bytes of a gzipped FileDescriptorProto
	0x1f, 0x8b, 0x08, 0x00, 0x00, 0x00, 0x00, 0x00, 0x02, 0xff, 0x5c, 0x8e, 0xdf, 0x4a, 0xc3, 0x30,
	0x18, 0xc5, 0xa9, 0x7f, 0x06, 0xfb, 0x86, 0xa0, 0x41, 0xdd, 0xa8, 0x20, 0x63, 0x20, 0x14, 0xa1,
	0x29, 0xce, 0x27, 0xb0, 0x0e, 0xdd, 0xee, 0x34, 0x7a, 0xe5, 0x5d, 0x9a, 0xc6, 0xa6, 0xb8, 0x35,
	0x25, 0x49, 0x2f, 0xfa, 0x9a, 0x3e, 0x91, 0x98, 0x64, 0xb5, 0xf6, 0x2a, 0x70, 0x7e, 0xbf, 0x93,
	0xf3, 0xc1, 0x55, 0xad, 0xa4, 0x91, 0xc9, 0x33, 0x37, 0x69, 0xfb, 0x26, 0xb7, 0x65, 0x5e, 0x9a,
	0x76, 0xb3, 0xc2, 0x36, 0x45, 0xc7, 0xf6, 0x09, 0x2f, 0x9d, 0x93, 0x52, 0x5d, 0xb2, 0xf7, 0xb6,
	0xe6, 0xda, 0xe1, 0x70, 0xea, 0xf2, 0xd7, 0x86, 0xab, 0x76, 0xcd, 0x69, 0xce, 0x95, 0x07, 0xa1,
	0x03, 0x84, 0xeb, 0x5a, 0x56, 0x9a, 0xf7, 0xd9, 0x22, 0x83, 0xf3, 0xc1, 0x98, 0xed, 0xa3, 0x5b,
	0x18, 0x09, 0xeb, 0xcd, 0x82, 0x79, 0x10, 0x4d, 0x96, 0xc8, 0xf9, 0xb8, 0xf7, 0x3b, 0xf1, 0x06,
	0xba, 0x06, 0xd0, 0x5d, 0x7d, 0x76, 0x30, 0x0f, 0xa2, 0x31, 0xe9, 0x25, 0x8b, 0xef, 0x00, 0xa6,
	0x83, 0x91, 0xfd, 0x2d, 0x28, 0x1e, 0xec, 0x5c, 0xf8, 0x9d, 0xff, 0xc7, 0x76, 0x53, 0x18, 0xc6,
	0x94, 0x31, 0xd9, 0x54, 0xc6, 0x2f, 0x4d, 0x96, 0xa7, 0xbe, 0xf1, 0xb0, 0xcf, 0xc9, 0x9f, 0x82,
	0x6e, 0x60, 0xf4, 0x59, 0x6e, 0xf9, 0x66, 0x35, 0x3b, 0xb4, 0xf2, 0x89, 0x97, 0x9f, 0x6c, 0x48,
	0x3c, 0x44, 0x77, 0x00, 0x4c, 0x56, 0x46, 0x51, 0xf6, 0xfb, 0xef, 0x91, 0x55, 0xcf, 0xbc, 0xfa,
	0xd8, 0x01, 0xd2, 0x93, 0xd2, 0x35, 0x84, 0x4c, 0xee, 0xb0, 0xe0, 0x39, 0x57, 0x14, 0x0b, 0xaa,
	0x45, 0xa1, 0x68, 0x2d, 0x5c, 0xe9, 0x25, 0xf8, 0x88, 0x8a, 0xd2, 0x88, 0x26, 0xc3, 0x4c, 0xee,
	0x92, 0x8e, 0x26, 0x4e, 0x8f, 0x75, 0xfe, 0x15, 0x17, 0x32, 0xb1, 0x6e, 0x36, 0xb2, 0xcf, 0xfd,
	0x4f, 0x00, 0x00, 0x00, 0xff, 0xff, 0x21, 0xf7, 0x63, 0x5e, 0xfc, 0x01, 0x00, 0x00,
}
