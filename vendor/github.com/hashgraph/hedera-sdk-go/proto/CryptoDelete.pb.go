// Code generated by protoc-gen-go. DO NOT EDIT.
// source: proto/CryptoDelete.proto

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

// Mark an account as deleted, moving all its current hbars to another account. It will remain in the ledger, marked as deleted, until it expires. Transfers into it a deleted account fail. But a deleted account can still have its expiration extended in the normal way.
type CryptoDeleteTransactionBody struct {
	TransferAccountID    *AccountID `protobuf:"bytes,1,opt,name=transferAccountID,proto3" json:"transferAccountID,omitempty"`
	DeleteAccountID      *AccountID `protobuf:"bytes,2,opt,name=deleteAccountID,proto3" json:"deleteAccountID,omitempty"`
	XXX_NoUnkeyedLiteral struct{}   `json:"-"`
	XXX_unrecognized     []byte     `json:"-"`
	XXX_sizecache        int32      `json:"-"`
}

func (m *CryptoDeleteTransactionBody) Reset()         { *m = CryptoDeleteTransactionBody{} }
func (m *CryptoDeleteTransactionBody) String() string { return proto.CompactTextString(m) }
func (*CryptoDeleteTransactionBody) ProtoMessage()    {}
func (*CryptoDeleteTransactionBody) Descriptor() ([]byte, []int) {
	return fileDescriptor_b469f4794c2f46fb, []int{0}
}

func (m *CryptoDeleteTransactionBody) XXX_Unmarshal(b []byte) error {
	return xxx_messageInfo_CryptoDeleteTransactionBody.Unmarshal(m, b)
}
func (m *CryptoDeleteTransactionBody) XXX_Marshal(b []byte, deterministic bool) ([]byte, error) {
	return xxx_messageInfo_CryptoDeleteTransactionBody.Marshal(b, m, deterministic)
}
func (m *CryptoDeleteTransactionBody) XXX_Merge(src proto.Message) {
	xxx_messageInfo_CryptoDeleteTransactionBody.Merge(m, src)
}
func (m *CryptoDeleteTransactionBody) XXX_Size() int {
	return xxx_messageInfo_CryptoDeleteTransactionBody.Size(m)
}
func (m *CryptoDeleteTransactionBody) XXX_DiscardUnknown() {
	xxx_messageInfo_CryptoDeleteTransactionBody.DiscardUnknown(m)
}

var xxx_messageInfo_CryptoDeleteTransactionBody proto.InternalMessageInfo

func (m *CryptoDeleteTransactionBody) GetTransferAccountID() *AccountID {
	if m != nil {
		return m.TransferAccountID
	}
	return nil
}

func (m *CryptoDeleteTransactionBody) GetDeleteAccountID() *AccountID {
	if m != nil {
		return m.DeleteAccountID
	}
	return nil
}

func init() {
	proto.RegisterType((*CryptoDeleteTransactionBody)(nil), "proto.CryptoDeleteTransactionBody")
}

func init() {
	proto.RegisterFile("proto/CryptoDelete.proto", fileDescriptor_b469f4794c2f46fb)
}

var fileDescriptor_b469f4794c2f46fb = []byte{
	// 199 bytes of a gzipped FileDescriptorProto
	0x1f, 0x8b, 0x08, 0x00, 0x00, 0x00, 0x00, 0x00, 0x02, 0xff, 0xe2, 0x92, 0x28, 0x28, 0xca, 0x2f,
	0xc9, 0xd7, 0x77, 0x2e, 0xaa, 0x2c, 0x28, 0xc9, 0x77, 0x49, 0xcd, 0x49, 0x2d, 0x49, 0xd5, 0x03,
	0x0b, 0x09, 0xb1, 0x82, 0x29, 0x29, 0x31, 0x88, 0x02, 0xa7, 0xc4, 0xe2, 0xcc, 0xe4, 0x90, 0xca,
	0x82, 0xd4, 0x62, 0x88, 0xb4, 0xd2, 0x4c, 0x46, 0x2e, 0x69, 0x64, 0x5d, 0x21, 0x45, 0x89, 0x79,
	0xc5, 0x89, 0xc9, 0x25, 0x99, 0xf9, 0x79, 0x4e, 0xf9, 0x29, 0x95, 0x42, 0x76, 0x5c, 0x82, 0x25,
	0x20, 0xa1, 0xb4, 0xd4, 0x22, 0xc7, 0xe4, 0xe4, 0xfc, 0xd2, 0xbc, 0x12, 0x4f, 0x17, 0x09, 0x46,
	0x05, 0x46, 0x0d, 0x6e, 0x23, 0x01, 0x88, 0x11, 0x7a, 0x70, 0xf1, 0x20, 0x4c, 0xa5, 0x42, 0x56,
	0x5c, 0xfc, 0x29, 0x60, 0x83, 0x11, 0xba, 0x99, 0x70, 0xe8, 0x46, 0x57, 0xe8, 0xe4, 0xc1, 0x25,
	0x95, 0x9c, 0x9f, 0xab, 0x97, 0x91, 0x9a, 0x92, 0x5a, 0x94, 0xa8, 0x97, 0x91, 0x58, 0x9c, 0x91,
	0x5e, 0x94, 0x58, 0x90, 0x01, 0xd1, 0x18, 0xc0, 0x18, 0xa5, 0x91, 0x9e, 0x59, 0x92, 0x51, 0x9a,
	0xa4, 0x97, 0x9c, 0x9f, 0xab, 0x0f, 0x97, 0xd5, 0x87, 0x28, 0xd7, 0x2d, 0x4e, 0xc9, 0xd6, 0x4d,
	0xcf, 0xd7, 0x07, 0xab, 0x4d, 0x62, 0x03, 0x53, 0xc6, 0x80, 0x00, 0x00, 0x00, 0xff, 0xff, 0xf2,
	0xd4, 0xd4, 0xe1, 0x27, 0x01, 0x00, 0x00,
}
