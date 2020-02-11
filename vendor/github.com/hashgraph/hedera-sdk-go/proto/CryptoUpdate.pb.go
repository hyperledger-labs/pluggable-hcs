// Code generated by protoc-gen-go. DO NOT EDIT.
// source: proto/CryptoUpdate.proto

package proto

import (
	fmt "fmt"
	proto "github.com/golang/protobuf/proto"
	wrappers "github.com/golang/protobuf/ptypes/wrappers"
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

// Change properties for the given account. Any null field is ignored (left unchanged). This transaction must be signed by the existing key for this account. If the transaction is changing the key field, then the transaction must be signed by both the old key (from before the change) and the new key. The old key must sign for security. The new key must sign as a safeguard to avoid accidentally changing to an invalid key, and then having no way to recover. When extending the expiration date, the cost is affected by the size of the list of attached claims, and of the keys associated with the claims and the account.
type CryptoUpdateTransactionBody struct {
	AccountIDToUpdate *AccountID `protobuf:"bytes,2,opt,name=accountIDToUpdate,proto3" json:"accountIDToUpdate,omitempty"`
	Key               *Key       `protobuf:"bytes,3,opt,name=key,proto3" json:"key,omitempty"`
	ProxyAccountID    *AccountID `protobuf:"bytes,4,opt,name=proxyAccountID,proto3" json:"proxyAccountID,omitempty"`
	ProxyFraction     int32      `protobuf:"varint,5,opt,name=proxyFraction,proto3" json:"proxyFraction,omitempty"` // Deprecated: Do not use.
	// Types that are valid to be assigned to SendRecordThresholdField:
	//	*CryptoUpdateTransactionBody_SendRecordThreshold
	//	*CryptoUpdateTransactionBody_SendRecordThresholdWrapper
	SendRecordThresholdField isCryptoUpdateTransactionBody_SendRecordThresholdField `protobuf_oneof:"sendRecordThresholdField"`
	// Types that are valid to be assigned to ReceiveRecordThresholdField:
	//	*CryptoUpdateTransactionBody_ReceiveRecordThreshold
	//	*CryptoUpdateTransactionBody_ReceiveRecordThresholdWrapper
	ReceiveRecordThresholdField isCryptoUpdateTransactionBody_ReceiveRecordThresholdField `protobuf_oneof:"receiveRecordThresholdField"`
	AutoRenewPeriod             *Duration                                                 `protobuf:"bytes,8,opt,name=autoRenewPeriod,proto3" json:"autoRenewPeriod,omitempty"`
	ExpirationTime              *Timestamp                                                `protobuf:"bytes,9,opt,name=expirationTime,proto3" json:"expirationTime,omitempty"`
	// Types that are valid to be assigned to ReceiverSigRequiredField:
	//	*CryptoUpdateTransactionBody_ReceiverSigRequired
	//	*CryptoUpdateTransactionBody_ReceiverSigRequiredWrapper
	ReceiverSigRequiredField isCryptoUpdateTransactionBody_ReceiverSigRequiredField `protobuf_oneof:"receiverSigRequiredField"`
	XXX_NoUnkeyedLiteral     struct{}                                               `json:"-"`
	XXX_unrecognized         []byte                                                 `json:"-"`
	XXX_sizecache            int32                                                  `json:"-"`
}

func (m *CryptoUpdateTransactionBody) Reset()         { *m = CryptoUpdateTransactionBody{} }
func (m *CryptoUpdateTransactionBody) String() string { return proto.CompactTextString(m) }
func (*CryptoUpdateTransactionBody) ProtoMessage()    {}
func (*CryptoUpdateTransactionBody) Descriptor() ([]byte, []int) {
	return fileDescriptor_dbfedcc8b947cb11, []int{0}
}

func (m *CryptoUpdateTransactionBody) XXX_Unmarshal(b []byte) error {
	return xxx_messageInfo_CryptoUpdateTransactionBody.Unmarshal(m, b)
}
func (m *CryptoUpdateTransactionBody) XXX_Marshal(b []byte, deterministic bool) ([]byte, error) {
	return xxx_messageInfo_CryptoUpdateTransactionBody.Marshal(b, m, deterministic)
}
func (m *CryptoUpdateTransactionBody) XXX_Merge(src proto.Message) {
	xxx_messageInfo_CryptoUpdateTransactionBody.Merge(m, src)
}
func (m *CryptoUpdateTransactionBody) XXX_Size() int {
	return xxx_messageInfo_CryptoUpdateTransactionBody.Size(m)
}
func (m *CryptoUpdateTransactionBody) XXX_DiscardUnknown() {
	xxx_messageInfo_CryptoUpdateTransactionBody.DiscardUnknown(m)
}

var xxx_messageInfo_CryptoUpdateTransactionBody proto.InternalMessageInfo

func (m *CryptoUpdateTransactionBody) GetAccountIDToUpdate() *AccountID {
	if m != nil {
		return m.AccountIDToUpdate
	}
	return nil
}

func (m *CryptoUpdateTransactionBody) GetKey() *Key {
	if m != nil {
		return m.Key
	}
	return nil
}

func (m *CryptoUpdateTransactionBody) GetProxyAccountID() *AccountID {
	if m != nil {
		return m.ProxyAccountID
	}
	return nil
}

// Deprecated: Do not use.
func (m *CryptoUpdateTransactionBody) GetProxyFraction() int32 {
	if m != nil {
		return m.ProxyFraction
	}
	return 0
}

type isCryptoUpdateTransactionBody_SendRecordThresholdField interface {
	isCryptoUpdateTransactionBody_SendRecordThresholdField()
}

type CryptoUpdateTransactionBody_SendRecordThreshold struct {
	SendRecordThreshold uint64 `protobuf:"varint,6,opt,name=sendRecordThreshold,proto3,oneof"`
}

type CryptoUpdateTransactionBody_SendRecordThresholdWrapper struct {
	SendRecordThresholdWrapper *wrappers.UInt64Value `protobuf:"bytes,11,opt,name=sendRecordThresholdWrapper,proto3,oneof"`
}

func (*CryptoUpdateTransactionBody_SendRecordThreshold) isCryptoUpdateTransactionBody_SendRecordThresholdField() {
}

func (*CryptoUpdateTransactionBody_SendRecordThresholdWrapper) isCryptoUpdateTransactionBody_SendRecordThresholdField() {
}

func (m *CryptoUpdateTransactionBody) GetSendRecordThresholdField() isCryptoUpdateTransactionBody_SendRecordThresholdField {
	if m != nil {
		return m.SendRecordThresholdField
	}
	return nil
}

// Deprecated: Do not use.
func (m *CryptoUpdateTransactionBody) GetSendRecordThreshold() uint64 {
	if x, ok := m.GetSendRecordThresholdField().(*CryptoUpdateTransactionBody_SendRecordThreshold); ok {
		return x.SendRecordThreshold
	}
	return 0
}

func (m *CryptoUpdateTransactionBody) GetSendRecordThresholdWrapper() *wrappers.UInt64Value {
	if x, ok := m.GetSendRecordThresholdField().(*CryptoUpdateTransactionBody_SendRecordThresholdWrapper); ok {
		return x.SendRecordThresholdWrapper
	}
	return nil
}

type isCryptoUpdateTransactionBody_ReceiveRecordThresholdField interface {
	isCryptoUpdateTransactionBody_ReceiveRecordThresholdField()
}

type CryptoUpdateTransactionBody_ReceiveRecordThreshold struct {
	ReceiveRecordThreshold uint64 `protobuf:"varint,7,opt,name=receiveRecordThreshold,proto3,oneof"`
}

type CryptoUpdateTransactionBody_ReceiveRecordThresholdWrapper struct {
	ReceiveRecordThresholdWrapper *wrappers.UInt64Value `protobuf:"bytes,12,opt,name=receiveRecordThresholdWrapper,proto3,oneof"`
}

func (*CryptoUpdateTransactionBody_ReceiveRecordThreshold) isCryptoUpdateTransactionBody_ReceiveRecordThresholdField() {
}

func (*CryptoUpdateTransactionBody_ReceiveRecordThresholdWrapper) isCryptoUpdateTransactionBody_ReceiveRecordThresholdField() {
}

func (m *CryptoUpdateTransactionBody) GetReceiveRecordThresholdField() isCryptoUpdateTransactionBody_ReceiveRecordThresholdField {
	if m != nil {
		return m.ReceiveRecordThresholdField
	}
	return nil
}

// Deprecated: Do not use.
func (m *CryptoUpdateTransactionBody) GetReceiveRecordThreshold() uint64 {
	if x, ok := m.GetReceiveRecordThresholdField().(*CryptoUpdateTransactionBody_ReceiveRecordThreshold); ok {
		return x.ReceiveRecordThreshold
	}
	return 0
}

func (m *CryptoUpdateTransactionBody) GetReceiveRecordThresholdWrapper() *wrappers.UInt64Value {
	if x, ok := m.GetReceiveRecordThresholdField().(*CryptoUpdateTransactionBody_ReceiveRecordThresholdWrapper); ok {
		return x.ReceiveRecordThresholdWrapper
	}
	return nil
}

func (m *CryptoUpdateTransactionBody) GetAutoRenewPeriod() *Duration {
	if m != nil {
		return m.AutoRenewPeriod
	}
	return nil
}

func (m *CryptoUpdateTransactionBody) GetExpirationTime() *Timestamp {
	if m != nil {
		return m.ExpirationTime
	}
	return nil
}

type isCryptoUpdateTransactionBody_ReceiverSigRequiredField interface {
	isCryptoUpdateTransactionBody_ReceiverSigRequiredField()
}

type CryptoUpdateTransactionBody_ReceiverSigRequired struct {
	ReceiverSigRequired bool `protobuf:"varint,10,opt,name=receiverSigRequired,proto3,oneof"`
}

type CryptoUpdateTransactionBody_ReceiverSigRequiredWrapper struct {
	ReceiverSigRequiredWrapper *wrappers.BoolValue `protobuf:"bytes,13,opt,name=receiverSigRequiredWrapper,proto3,oneof"`
}

func (*CryptoUpdateTransactionBody_ReceiverSigRequired) isCryptoUpdateTransactionBody_ReceiverSigRequiredField() {
}

func (*CryptoUpdateTransactionBody_ReceiverSigRequiredWrapper) isCryptoUpdateTransactionBody_ReceiverSigRequiredField() {
}

func (m *CryptoUpdateTransactionBody) GetReceiverSigRequiredField() isCryptoUpdateTransactionBody_ReceiverSigRequiredField {
	if m != nil {
		return m.ReceiverSigRequiredField
	}
	return nil
}

// Deprecated: Do not use.
func (m *CryptoUpdateTransactionBody) GetReceiverSigRequired() bool {
	if x, ok := m.GetReceiverSigRequiredField().(*CryptoUpdateTransactionBody_ReceiverSigRequired); ok {
		return x.ReceiverSigRequired
	}
	return false
}

func (m *CryptoUpdateTransactionBody) GetReceiverSigRequiredWrapper() *wrappers.BoolValue {
	if x, ok := m.GetReceiverSigRequiredField().(*CryptoUpdateTransactionBody_ReceiverSigRequiredWrapper); ok {
		return x.ReceiverSigRequiredWrapper
	}
	return nil
}

// XXX_OneofWrappers is for the internal use of the proto package.
func (*CryptoUpdateTransactionBody) XXX_OneofWrappers() []interface{} {
	return []interface{}{
		(*CryptoUpdateTransactionBody_SendRecordThreshold)(nil),
		(*CryptoUpdateTransactionBody_SendRecordThresholdWrapper)(nil),
		(*CryptoUpdateTransactionBody_ReceiveRecordThreshold)(nil),
		(*CryptoUpdateTransactionBody_ReceiveRecordThresholdWrapper)(nil),
		(*CryptoUpdateTransactionBody_ReceiverSigRequired)(nil),
		(*CryptoUpdateTransactionBody_ReceiverSigRequiredWrapper)(nil),
	}
}

func init() {
	proto.RegisterType((*CryptoUpdateTransactionBody)(nil), "proto.CryptoUpdateTransactionBody")
}

func init() { proto.RegisterFile("proto/CryptoUpdate.proto", fileDescriptor_dbfedcc8b947cb11) }

var fileDescriptor_dbfedcc8b947cb11 = []byte{
	// 491 bytes of a gzipped FileDescriptorProto
	0x1f, 0x8b, 0x08, 0x00, 0x00, 0x00, 0x00, 0x00, 0x02, 0xff, 0x8c, 0x53, 0x51, 0x6b, 0xd4, 0x40,
	0x18, 0x6c, 0xae, 0xbd, 0xb3, 0x6e, 0xad, 0xd5, 0x55, 0xcb, 0x92, 0xb6, 0x72, 0xf8, 0x94, 0x97,
	0x26, 0xa0, 0x52, 0x14, 0x44, 0x30, 0x96, 0x72, 0xc5, 0x97, 0xb2, 0xa6, 0x0a, 0x22, 0xc2, 0x36,
	0xfb, 0x99, 0x2c, 0xcd, 0x65, 0xd7, 0xcd, 0xc6, 0x36, 0x3f, 0x5e, 0x90, 0xec, 0x26, 0x87, 0xbd,
	0xa6, 0x87, 0x4f, 0xcb, 0xcd, 0xcc, 0x37, 0x37, 0xcc, 0xf7, 0x05, 0x11, 0xa5, 0xa5, 0x91, 0xd1,
	0x47, 0xdd, 0x28, 0x23, 0xcf, 0x15, 0x67, 0x06, 0x42, 0x0b, 0xe1, 0xb1, 0x7d, 0xfc, 0x5d, 0x27,
	0x88, 0x59, 0x25, 0xd2, 0xa4, 0x51, 0x50, 0x39, 0xda, 0x7f, 0xea, 0xf0, 0xe3, 0x5a, 0x33, 0x23,
	0x64, 0xd9, 0xa1, 0xcf, 0x1c, 0x9a, 0x88, 0x39, 0x54, 0x86, 0xcd, 0x55, 0x07, 0x3f, 0xcf, 0xa4,
	0xcc, 0x0a, 0x88, 0xec, 0xaf, 0x8b, 0xfa, 0x67, 0x74, 0xa5, 0x99, 0x52, 0xa0, 0x3b, 0xb3, 0x17,
	0x7f, 0x26, 0x68, 0xef, 0xdf, 0x08, 0x89, 0x66, 0x65, 0xc5, 0xd2, 0xd6, 0x38, 0x96, 0xbc, 0xc1,
	0xef, 0xd1, 0x63, 0x96, 0xa6, 0xb2, 0x2e, 0xcd, 0xe9, 0x71, 0xd2, 0x69, 0xc8, 0x68, 0xea, 0x05,
	0x5b, 0x2f, 0x1f, 0x39, 0x8b, 0xf0, 0x43, 0xcf, 0xd3, 0xdb, 0x52, 0xbc, 0x8f, 0xd6, 0x2f, 0xa1,
	0x21, 0xeb, 0x76, 0x02, 0x75, 0x13, 0x9f, 0xa0, 0xa1, 0x2d, 0x8c, 0xdf, 0xa0, 0x87, 0x4a, 0xcb,
	0xeb, 0x66, 0x61, 0x41, 0x36, 0xee, 0xb0, 0x5e, 0xd2, 0xe1, 0x00, 0x6d, 0x5b, 0xe4, 0x44, 0xbb,
	0xb0, 0x64, 0x3c, 0xf5, 0x82, 0x71, 0x3c, 0x22, 0x1e, 0xbd, 0x49, 0xe0, 0x23, 0xf4, 0xa4, 0x82,
	0x92, 0x53, 0x48, 0xa5, 0xe6, 0x49, 0xae, 0xa1, 0xca, 0x65, 0xc1, 0xc9, 0x64, 0xea, 0x05, 0x1b,
	0xad, 0x7e, 0xb6, 0x46, 0x87, 0x04, 0xf8, 0x07, 0xf2, 0x07, 0xe0, 0xaf, 0xae, 0x3e, 0xb2, 0x65,
	0x73, 0xee, 0x87, 0xae, 0xde, 0xb0, 0xaf, 0x37, 0x3c, 0x3f, 0x2d, 0xcd, 0xd1, 0xeb, 0x2f, 0xac,
	0xa8, 0x61, 0xb6, 0x46, 0x57, 0x38, 0xe0, 0x77, 0x68, 0x57, 0x43, 0x0a, 0xe2, 0x37, 0x2c, 0x47,
	0xbb, 0xb7, 0x88, 0xe6, 0xd1, 0x3b, 0x34, 0x98, 0xa3, 0x83, 0x61, 0xa6, 0x0f, 0xf8, 0xe0, 0x3f,
	0x02, 0x7a, 0x74, 0xb5, 0x09, 0x7e, 0x8b, 0x76, 0x58, 0x6d, 0x24, 0x85, 0x12, 0xae, 0xce, 0x40,
	0x0b, 0xc9, 0xc9, 0xa6, 0xf5, 0xdd, 0xe9, 0x16, 0xd4, 0x1f, 0x21, 0x5d, 0xd6, 0xb5, 0xab, 0x85,
	0x6b, 0x25, 0x1c, 0xdd, 0x5e, 0x25, 0xb9, 0x7f, 0x63, 0xb5, 0x8b, 0x43, 0xa5, 0x4b, 0xba, 0x76,
	0x61, 0x5d, 0x2a, 0xfd, 0x59, 0x64, 0x14, 0x7e, 0xd5, 0x42, 0x03, 0x27, 0x68, 0xea, 0x05, 0x9b,
	0xb6, 0x95, 0x11, 0x1d, 0x12, 0xe0, 0xef, 0xc8, 0x1f, 0x80, 0xfb, 0x3e, 0xb6, 0xed, 0xbf, 0xfb,
	0xb7, 0xfa, 0x88, 0xa5, 0x2c, 0x5c, 0x1b, 0x23, 0xba, 0x62, 0x3e, 0xf6, 0x11, 0x19, 0x58, 0xe6,
	0x89, 0x80, 0x82, 0xc7, 0x07, 0x68, 0x6f, 0xb8, 0x47, 0x47, 0xfb, 0x88, 0x0c, 0x18, 0x3b, 0x6e,
	0x86, 0xfc, 0x54, 0xce, 0xc3, 0x1c, 0x38, 0x68, 0x16, 0xe6, 0xac, 0xca, 0x33, 0xcd, 0x54, 0xee,
	0xf2, 0x9d, 0x79, 0xdf, 0x82, 0x4c, 0x98, 0xbc, 0xbe, 0x08, 0x53, 0x39, 0x8f, 0x16, 0x6c, 0xe4,
	0xe4, 0x87, 0x15, 0xbf, 0x3c, 0xcc, 0x64, 0xf7, 0x6d, 0x4f, 0xec, 0xf3, 0xea, 0x6f, 0x00, 0x00,
	0x00, 0xff, 0xff, 0x98, 0x09, 0x78, 0x38, 0x58, 0x04, 0x00, 0x00,
}