// Code generated by protoc-gen-go. DO NOT EDIT.
// source: proto/FileUpdate.proto

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

// Modify some of the metadata for a file. Any null field is ignored (left unchanged). Any field that is null is left unchanged. If contents is non-null, then the file's contents will be replaced with the given bytes. This transaction must be signed by all the keys for that file. If the transaction is modifying the keys field, then it must be signed by all the keys in both the old list and the new list.
//
// If a file was created without ANY keys in the keys field, ONLY the expirationTime of the file can be changed using this call. The file contents or its keys cannot be changed.
type FileUpdateTransactionBody struct {
	FileID               *FileID    `protobuf:"bytes,1,opt,name=fileID,proto3" json:"fileID,omitempty"`
	ExpirationTime       *Timestamp `protobuf:"bytes,2,opt,name=expirationTime,proto3" json:"expirationTime,omitempty"`
	Keys                 *KeyList   `protobuf:"bytes,3,opt,name=keys,proto3" json:"keys,omitempty"`
	Contents             []byte     `protobuf:"bytes,4,opt,name=contents,proto3" json:"contents,omitempty"`
	XXX_NoUnkeyedLiteral struct{}   `json:"-"`
	XXX_unrecognized     []byte     `json:"-"`
	XXX_sizecache        int32      `json:"-"`
}

func (m *FileUpdateTransactionBody) Reset()         { *m = FileUpdateTransactionBody{} }
func (m *FileUpdateTransactionBody) String() string { return proto.CompactTextString(m) }
func (*FileUpdateTransactionBody) ProtoMessage()    {}
func (*FileUpdateTransactionBody) Descriptor() ([]byte, []int) {
	return fileDescriptor_4bd74cf52e0b01ce, []int{0}
}

func (m *FileUpdateTransactionBody) XXX_Unmarshal(b []byte) error {
	return xxx_messageInfo_FileUpdateTransactionBody.Unmarshal(m, b)
}
func (m *FileUpdateTransactionBody) XXX_Marshal(b []byte, deterministic bool) ([]byte, error) {
	return xxx_messageInfo_FileUpdateTransactionBody.Marshal(b, m, deterministic)
}
func (m *FileUpdateTransactionBody) XXX_Merge(src proto.Message) {
	xxx_messageInfo_FileUpdateTransactionBody.Merge(m, src)
}
func (m *FileUpdateTransactionBody) XXX_Size() int {
	return xxx_messageInfo_FileUpdateTransactionBody.Size(m)
}
func (m *FileUpdateTransactionBody) XXX_DiscardUnknown() {
	xxx_messageInfo_FileUpdateTransactionBody.DiscardUnknown(m)
}

var xxx_messageInfo_FileUpdateTransactionBody proto.InternalMessageInfo

func (m *FileUpdateTransactionBody) GetFileID() *FileID {
	if m != nil {
		return m.FileID
	}
	return nil
}

func (m *FileUpdateTransactionBody) GetExpirationTime() *Timestamp {
	if m != nil {
		return m.ExpirationTime
	}
	return nil
}

func (m *FileUpdateTransactionBody) GetKeys() *KeyList {
	if m != nil {
		return m.Keys
	}
	return nil
}

func (m *FileUpdateTransactionBody) GetContents() []byte {
	if m != nil {
		return m.Contents
	}
	return nil
}

func init() {
	proto.RegisterType((*FileUpdateTransactionBody)(nil), "proto.FileUpdateTransactionBody")
}

func init() {
	proto.RegisterFile("proto/FileUpdate.proto", fileDescriptor_4bd74cf52e0b01ce)
}

var fileDescriptor_4bd74cf52e0b01ce = []byte{
	// 251 bytes of a gzipped FileDescriptorProto
	0x1f, 0x8b, 0x08, 0x00, 0x00, 0x00, 0x00, 0x00, 0x02, 0xff, 0x5c, 0x8d, 0xc1, 0x4a, 0xc4, 0x30,
	0x10, 0x86, 0xa9, 0xae, 0x8b, 0x44, 0x5d, 0x24, 0xa0, 0xd4, 0x9e, 0x96, 0x05, 0xa1, 0x97, 0x4d,
	0x41, 0x2f, 0x9e, 0x8b, 0x2c, 0x8a, 0x1e, 0xa4, 0xd4, 0x8b, 0xb7, 0xd9, 0x74, 0x6c, 0xc2, 0x6e,
	0x9b, 0x90, 0x89, 0x60, 0xdf, 0xcd, 0x87, 0x93, 0x4d, 0x4a, 0x05, 0x4f, 0xc3, 0xfc, 0xdf, 0x37,
	0xf3, 0xb3, 0x6b, 0xeb, 0x8c, 0x37, 0xc5, 0x46, 0xef, 0xf1, 0xdd, 0x36, 0xe0, 0x51, 0x84, 0x80,
	0x9f, 0x84, 0x91, 0x8d, 0xb8, 0x04, 0xd2, 0xb2, 0x1e, 0x2c, 0x52, 0xc4, 0xd9, 0x55, 0xcc, 0x6b,
	0xdd, 0x21, 0x79, 0xe8, 0x6c, 0x8c, 0x57, 0x3f, 0x09, 0xbb, 0xf9, 0x7b, 0x55, 0x3b, 0xe8, 0x09,
	0xa4, 0xd7, 0xa6, 0x2f, 0x4d, 0x33, 0xf0, 0x5b, 0x36, 0xff, 0xd4, 0x7b, 0x7c, 0x7e, 0x4c, 0x93,
	0x65, 0x92, 0x9f, 0xdd, 0x5d, 0xc4, 0x2b, 0xb1, 0x09, 0x61, 0x35, 0x42, 0xfe, 0xc0, 0x16, 0xf8,
	0x6d, 0xb5, 0x83, 0xc3, 0xe1, 0xa1, 0x21, 0x3d, 0x0a, 0xfa, 0xe5, 0xa8, 0x4f, 0xa5, 0xd5, 0x3f,
	0x8f, 0xaf, 0xd8, 0x6c, 0x87, 0x03, 0xa5, 0xc7, 0xc1, 0x5f, 0x8c, 0xfe, 0x0b, 0x0e, 0xaf, 0x9a,
	0x7c, 0x15, 0x18, 0xcf, 0xd8, 0xa9, 0x34, 0xbd, 0xc7, 0xde, 0x53, 0x3a, 0x5b, 0x26, 0xf9, 0x79,
	0x35, 0xed, 0xe5, 0x13, 0xcb, 0xa4, 0xe9, 0x84, 0xc2, 0x06, 0x1d, 0x08, 0x05, 0xa4, 0x5a, 0x07,
	0x56, 0xc5, 0x3f, 0x6f, 0xc9, 0x47, 0xde, 0x6a, 0xaf, 0xbe, 0xb6, 0x42, 0x9a, 0xae, 0x98, 0x68,
	0x11, 0xf5, 0x35, 0x35, 0xbb, 0x75, 0x6b, 0x8a, 0xe0, 0x6e, 0xe7, 0x61, 0xdc, 0xff, 0x06, 0x00,
	0x00, 0xff, 0xff, 0xb7, 0x69, 0x0e, 0x44, 0x5f, 0x01, 0x00, 0x00,
}