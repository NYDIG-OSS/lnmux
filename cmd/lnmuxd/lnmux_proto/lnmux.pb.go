// Code generated by protoc-gen-go. DO NOT EDIT.
// versions:
// 	protoc-gen-go v1.27.1
// 	protoc        v3.18.0
// source: lnmux.proto

package lnmux_proto

import (
	protoreflect "google.golang.org/protobuf/reflect/protoreflect"
	protoimpl "google.golang.org/protobuf/runtime/protoimpl"
	reflect "reflect"
	sync "sync"
)

const (
	// Verify that this generated code is sufficiently up-to-date.
	_ = protoimpl.EnforceVersion(20 - protoimpl.MinVersion)
	// Verify that runtime/protoimpl is sufficiently up-to-date.
	_ = protoimpl.EnforceVersion(protoimpl.MaxVersion - 20)
)

type SubscribeSingleInvoiceResponse_InvoiceState int32

const (
	SubscribeSingleInvoiceResponse_STATE_OPEN             SubscribeSingleInvoiceResponse_InvoiceState = 0
	SubscribeSingleInvoiceResponse_STATE_ACCEPTED         SubscribeSingleInvoiceResponse_InvoiceState = 1
	SubscribeSingleInvoiceResponse_STATE_SETTLE_REQUESTED SubscribeSingleInvoiceResponse_InvoiceState = 2
	SubscribeSingleInvoiceResponse_STATE_SETTLED          SubscribeSingleInvoiceResponse_InvoiceState = 3
	SubscribeSingleInvoiceResponse_STATE_EXPIRED          SubscribeSingleInvoiceResponse_InvoiceState = 4
	SubscribeSingleInvoiceResponse_STATE_CANCELLED        SubscribeSingleInvoiceResponse_InvoiceState = 5
)

// Enum value maps for SubscribeSingleInvoiceResponse_InvoiceState.
var (
	SubscribeSingleInvoiceResponse_InvoiceState_name = map[int32]string{
		0: "STATE_OPEN",
		1: "STATE_ACCEPTED",
		2: "STATE_SETTLE_REQUESTED",
		3: "STATE_SETTLED",
		4: "STATE_EXPIRED",
		5: "STATE_CANCELLED",
	}
	SubscribeSingleInvoiceResponse_InvoiceState_value = map[string]int32{
		"STATE_OPEN":             0,
		"STATE_ACCEPTED":         1,
		"STATE_SETTLE_REQUESTED": 2,
		"STATE_SETTLED":          3,
		"STATE_EXPIRED":          4,
		"STATE_CANCELLED":        5,
	}
)

func (x SubscribeSingleInvoiceResponse_InvoiceState) Enum() *SubscribeSingleInvoiceResponse_InvoiceState {
	p := new(SubscribeSingleInvoiceResponse_InvoiceState)
	*p = x
	return p
}

func (x SubscribeSingleInvoiceResponse_InvoiceState) String() string {
	return protoimpl.X.EnumStringOf(x.Descriptor(), protoreflect.EnumNumber(x))
}

func (SubscribeSingleInvoiceResponse_InvoiceState) Descriptor() protoreflect.EnumDescriptor {
	return file_lnmux_proto_enumTypes[0].Descriptor()
}

func (SubscribeSingleInvoiceResponse_InvoiceState) Type() protoreflect.EnumType {
	return &file_lnmux_proto_enumTypes[0]
}

func (x SubscribeSingleInvoiceResponse_InvoiceState) Number() protoreflect.EnumNumber {
	return protoreflect.EnumNumber(x)
}

// Deprecated: Use SubscribeSingleInvoiceResponse_InvoiceState.Descriptor instead.
func (SubscribeSingleInvoiceResponse_InvoiceState) EnumDescriptor() ([]byte, []int) {
	return file_lnmux_proto_rawDescGZIP(), []int{3, 0}
}

type SubscribeSingleInvoiceResponse_CancelledReason int32

const (
	SubscribeSingleInvoiceResponse_REASON_NONE           SubscribeSingleInvoiceResponse_CancelledReason = 0
	SubscribeSingleInvoiceResponse_REASON_EXPIRED        SubscribeSingleInvoiceResponse_CancelledReason = 1
	SubscribeSingleInvoiceResponse_REASON_ACCEPT_TIMEOUT SubscribeSingleInvoiceResponse_CancelledReason = 2
	SubscribeSingleInvoiceResponse_REASON_EXTERNAL       SubscribeSingleInvoiceResponse_CancelledReason = 3
)

// Enum value maps for SubscribeSingleInvoiceResponse_CancelledReason.
var (
	SubscribeSingleInvoiceResponse_CancelledReason_name = map[int32]string{
		0: "REASON_NONE",
		1: "REASON_EXPIRED",
		2: "REASON_ACCEPT_TIMEOUT",
		3: "REASON_EXTERNAL",
	}
	SubscribeSingleInvoiceResponse_CancelledReason_value = map[string]int32{
		"REASON_NONE":           0,
		"REASON_EXPIRED":        1,
		"REASON_ACCEPT_TIMEOUT": 2,
		"REASON_EXTERNAL":       3,
	}
)

func (x SubscribeSingleInvoiceResponse_CancelledReason) Enum() *SubscribeSingleInvoiceResponse_CancelledReason {
	p := new(SubscribeSingleInvoiceResponse_CancelledReason)
	*p = x
	return p
}

func (x SubscribeSingleInvoiceResponse_CancelledReason) String() string {
	return protoimpl.X.EnumStringOf(x.Descriptor(), protoreflect.EnumNumber(x))
}

func (SubscribeSingleInvoiceResponse_CancelledReason) Descriptor() protoreflect.EnumDescriptor {
	return file_lnmux_proto_enumTypes[1].Descriptor()
}

func (SubscribeSingleInvoiceResponse_CancelledReason) Type() protoreflect.EnumType {
	return &file_lnmux_proto_enumTypes[1]
}

func (x SubscribeSingleInvoiceResponse_CancelledReason) Number() protoreflect.EnumNumber {
	return protoreflect.EnumNumber(x)
}

// Deprecated: Use SubscribeSingleInvoiceResponse_CancelledReason.Descriptor instead.
func (SubscribeSingleInvoiceResponse_CancelledReason) EnumDescriptor() ([]byte, []int) {
	return file_lnmux_proto_rawDescGZIP(), []int{3, 1}
}

type AddInvoiceRequest struct {
	state         protoimpl.MessageState
	sizeCache     protoimpl.SizeCache
	unknownFields protoimpl.UnknownFields

	AmtMsat         int64  `protobuf:"varint,1,opt,name=amt_msat,json=amtMsat,proto3" json:"amt_msat,omitempty"`
	Description     string `protobuf:"bytes,2,opt,name=description,proto3" json:"description,omitempty"`
	DescriptionHash []byte `protobuf:"bytes,3,opt,name=description_hash,json=descriptionHash,proto3" json:"description_hash,omitempty"`
	ExpirySecs      int64  `protobuf:"varint,4,opt,name=expiry_secs,json=expirySecs,proto3" json:"expiry_secs,omitempty"`
}

func (x *AddInvoiceRequest) Reset() {
	*x = AddInvoiceRequest{}
	if protoimpl.UnsafeEnabled {
		mi := &file_lnmux_proto_msgTypes[0]
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		ms.StoreMessageInfo(mi)
	}
}

func (x *AddInvoiceRequest) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*AddInvoiceRequest) ProtoMessage() {}

func (x *AddInvoiceRequest) ProtoReflect() protoreflect.Message {
	mi := &file_lnmux_proto_msgTypes[0]
	if protoimpl.UnsafeEnabled && x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use AddInvoiceRequest.ProtoReflect.Descriptor instead.
func (*AddInvoiceRequest) Descriptor() ([]byte, []int) {
	return file_lnmux_proto_rawDescGZIP(), []int{0}
}

func (x *AddInvoiceRequest) GetAmtMsat() int64 {
	if x != nil {
		return x.AmtMsat
	}
	return 0
}

func (x *AddInvoiceRequest) GetDescription() string {
	if x != nil {
		return x.Description
	}
	return ""
}

func (x *AddInvoiceRequest) GetDescriptionHash() []byte {
	if x != nil {
		return x.DescriptionHash
	}
	return nil
}

func (x *AddInvoiceRequest) GetExpirySecs() int64 {
	if x != nil {
		return x.ExpirySecs
	}
	return 0
}

type AddInvoiceResponse struct {
	state         protoimpl.MessageState
	sizeCache     protoimpl.SizeCache
	unknownFields protoimpl.UnknownFields

	PaymentRequest string `protobuf:"bytes,1,opt,name=payment_request,json=paymentRequest,proto3" json:"payment_request,omitempty"`
	Preimage       []byte `protobuf:"bytes,2,opt,name=preimage,proto3" json:"preimage,omitempty"`
	Hash           []byte `protobuf:"bytes,3,opt,name=hash,proto3" json:"hash,omitempty"`
}

func (x *AddInvoiceResponse) Reset() {
	*x = AddInvoiceResponse{}
	if protoimpl.UnsafeEnabled {
		mi := &file_lnmux_proto_msgTypes[1]
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		ms.StoreMessageInfo(mi)
	}
}

func (x *AddInvoiceResponse) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*AddInvoiceResponse) ProtoMessage() {}

func (x *AddInvoiceResponse) ProtoReflect() protoreflect.Message {
	mi := &file_lnmux_proto_msgTypes[1]
	if protoimpl.UnsafeEnabled && x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use AddInvoiceResponse.ProtoReflect.Descriptor instead.
func (*AddInvoiceResponse) Descriptor() ([]byte, []int) {
	return file_lnmux_proto_rawDescGZIP(), []int{1}
}

func (x *AddInvoiceResponse) GetPaymentRequest() string {
	if x != nil {
		return x.PaymentRequest
	}
	return ""
}

func (x *AddInvoiceResponse) GetPreimage() []byte {
	if x != nil {
		return x.Preimage
	}
	return nil
}

func (x *AddInvoiceResponse) GetHash() []byte {
	if x != nil {
		return x.Hash
	}
	return nil
}

type SubscribeSingleInvoiceRequest struct {
	state         protoimpl.MessageState
	sizeCache     protoimpl.SizeCache
	unknownFields protoimpl.UnknownFields

	Hash []byte `protobuf:"bytes,1,opt,name=hash,proto3" json:"hash,omitempty"`
}

func (x *SubscribeSingleInvoiceRequest) Reset() {
	*x = SubscribeSingleInvoiceRequest{}
	if protoimpl.UnsafeEnabled {
		mi := &file_lnmux_proto_msgTypes[2]
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		ms.StoreMessageInfo(mi)
	}
}

func (x *SubscribeSingleInvoiceRequest) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*SubscribeSingleInvoiceRequest) ProtoMessage() {}

func (x *SubscribeSingleInvoiceRequest) ProtoReflect() protoreflect.Message {
	mi := &file_lnmux_proto_msgTypes[2]
	if protoimpl.UnsafeEnabled && x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use SubscribeSingleInvoiceRequest.ProtoReflect.Descriptor instead.
func (*SubscribeSingleInvoiceRequest) Descriptor() ([]byte, []int) {
	return file_lnmux_proto_rawDescGZIP(), []int{2}
}

func (x *SubscribeSingleInvoiceRequest) GetHash() []byte {
	if x != nil {
		return x.Hash
	}
	return nil
}

type SubscribeSingleInvoiceResponse struct {
	state         protoimpl.MessageState
	sizeCache     protoimpl.SizeCache
	unknownFields protoimpl.UnknownFields

	State           SubscribeSingleInvoiceResponse_InvoiceState    `protobuf:"varint,1,opt,name=state,proto3,enum=lnmux.SubscribeSingleInvoiceResponse_InvoiceState" json:"state,omitempty"`
	CancelledReason SubscribeSingleInvoiceResponse_CancelledReason `protobuf:"varint,2,opt,name=cancelled_reason,json=cancelledReason,proto3,enum=lnmux.SubscribeSingleInvoiceResponse_CancelledReason" json:"cancelled_reason,omitempty"`
}

func (x *SubscribeSingleInvoiceResponse) Reset() {
	*x = SubscribeSingleInvoiceResponse{}
	if protoimpl.UnsafeEnabled {
		mi := &file_lnmux_proto_msgTypes[3]
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		ms.StoreMessageInfo(mi)
	}
}

func (x *SubscribeSingleInvoiceResponse) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*SubscribeSingleInvoiceResponse) ProtoMessage() {}

func (x *SubscribeSingleInvoiceResponse) ProtoReflect() protoreflect.Message {
	mi := &file_lnmux_proto_msgTypes[3]
	if protoimpl.UnsafeEnabled && x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use SubscribeSingleInvoiceResponse.ProtoReflect.Descriptor instead.
func (*SubscribeSingleInvoiceResponse) Descriptor() ([]byte, []int) {
	return file_lnmux_proto_rawDescGZIP(), []int{3}
}

func (x *SubscribeSingleInvoiceResponse) GetState() SubscribeSingleInvoiceResponse_InvoiceState {
	if x != nil {
		return x.State
	}
	return SubscribeSingleInvoiceResponse_STATE_OPEN
}

func (x *SubscribeSingleInvoiceResponse) GetCancelledReason() SubscribeSingleInvoiceResponse_CancelledReason {
	if x != nil {
		return x.CancelledReason
	}
	return SubscribeSingleInvoiceResponse_REASON_NONE
}

type SettleInvoiceRequest struct {
	state         protoimpl.MessageState
	sizeCache     protoimpl.SizeCache
	unknownFields protoimpl.UnknownFields

	Hash []byte `protobuf:"bytes,1,opt,name=hash,proto3" json:"hash,omitempty"`
}

func (x *SettleInvoiceRequest) Reset() {
	*x = SettleInvoiceRequest{}
	if protoimpl.UnsafeEnabled {
		mi := &file_lnmux_proto_msgTypes[4]
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		ms.StoreMessageInfo(mi)
	}
}

func (x *SettleInvoiceRequest) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*SettleInvoiceRequest) ProtoMessage() {}

func (x *SettleInvoiceRequest) ProtoReflect() protoreflect.Message {
	mi := &file_lnmux_proto_msgTypes[4]
	if protoimpl.UnsafeEnabled && x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use SettleInvoiceRequest.ProtoReflect.Descriptor instead.
func (*SettleInvoiceRequest) Descriptor() ([]byte, []int) {
	return file_lnmux_proto_rawDescGZIP(), []int{4}
}

func (x *SettleInvoiceRequest) GetHash() []byte {
	if x != nil {
		return x.Hash
	}
	return nil
}

type SettleInvoiceResponse struct {
	state         protoimpl.MessageState
	sizeCache     protoimpl.SizeCache
	unknownFields protoimpl.UnknownFields
}

func (x *SettleInvoiceResponse) Reset() {
	*x = SettleInvoiceResponse{}
	if protoimpl.UnsafeEnabled {
		mi := &file_lnmux_proto_msgTypes[5]
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		ms.StoreMessageInfo(mi)
	}
}

func (x *SettleInvoiceResponse) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*SettleInvoiceResponse) ProtoMessage() {}

func (x *SettleInvoiceResponse) ProtoReflect() protoreflect.Message {
	mi := &file_lnmux_proto_msgTypes[5]
	if protoimpl.UnsafeEnabled && x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use SettleInvoiceResponse.ProtoReflect.Descriptor instead.
func (*SettleInvoiceResponse) Descriptor() ([]byte, []int) {
	return file_lnmux_proto_rawDescGZIP(), []int{5}
}

type CancelInvoiceRequest struct {
	state         protoimpl.MessageState
	sizeCache     protoimpl.SizeCache
	unknownFields protoimpl.UnknownFields

	Hash []byte `protobuf:"bytes,1,opt,name=hash,proto3" json:"hash,omitempty"`
}

func (x *CancelInvoiceRequest) Reset() {
	*x = CancelInvoiceRequest{}
	if protoimpl.UnsafeEnabled {
		mi := &file_lnmux_proto_msgTypes[6]
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		ms.StoreMessageInfo(mi)
	}
}

func (x *CancelInvoiceRequest) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*CancelInvoiceRequest) ProtoMessage() {}

func (x *CancelInvoiceRequest) ProtoReflect() protoreflect.Message {
	mi := &file_lnmux_proto_msgTypes[6]
	if protoimpl.UnsafeEnabled && x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use CancelInvoiceRequest.ProtoReflect.Descriptor instead.
func (*CancelInvoiceRequest) Descriptor() ([]byte, []int) {
	return file_lnmux_proto_rawDescGZIP(), []int{6}
}

func (x *CancelInvoiceRequest) GetHash() []byte {
	if x != nil {
		return x.Hash
	}
	return nil
}

type CancelInvoiceResponse struct {
	state         protoimpl.MessageState
	sizeCache     protoimpl.SizeCache
	unknownFields protoimpl.UnknownFields
}

func (x *CancelInvoiceResponse) Reset() {
	*x = CancelInvoiceResponse{}
	if protoimpl.UnsafeEnabled {
		mi := &file_lnmux_proto_msgTypes[7]
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		ms.StoreMessageInfo(mi)
	}
}

func (x *CancelInvoiceResponse) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*CancelInvoiceResponse) ProtoMessage() {}

func (x *CancelInvoiceResponse) ProtoReflect() protoreflect.Message {
	mi := &file_lnmux_proto_msgTypes[7]
	if protoimpl.UnsafeEnabled && x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use CancelInvoiceResponse.ProtoReflect.Descriptor instead.
func (*CancelInvoiceResponse) Descriptor() ([]byte, []int) {
	return file_lnmux_proto_rawDescGZIP(), []int{7}
}

var File_lnmux_proto protoreflect.FileDescriptor

var file_lnmux_proto_rawDesc = []byte{
	0x0a, 0x0b, 0x6c, 0x6e, 0x6d, 0x75, 0x78, 0x2e, 0x70, 0x72, 0x6f, 0x74, 0x6f, 0x12, 0x05, 0x6c,
	0x6e, 0x6d, 0x75, 0x78, 0x22, 0x9c, 0x01, 0x0a, 0x11, 0x41, 0x64, 0x64, 0x49, 0x6e, 0x76, 0x6f,
	0x69, 0x63, 0x65, 0x52, 0x65, 0x71, 0x75, 0x65, 0x73, 0x74, 0x12, 0x19, 0x0a, 0x08, 0x61, 0x6d,
	0x74, 0x5f, 0x6d, 0x73, 0x61, 0x74, 0x18, 0x01, 0x20, 0x01, 0x28, 0x03, 0x52, 0x07, 0x61, 0x6d,
	0x74, 0x4d, 0x73, 0x61, 0x74, 0x12, 0x20, 0x0a, 0x0b, 0x64, 0x65, 0x73, 0x63, 0x72, 0x69, 0x70,
	0x74, 0x69, 0x6f, 0x6e, 0x18, 0x02, 0x20, 0x01, 0x28, 0x09, 0x52, 0x0b, 0x64, 0x65, 0x73, 0x63,
	0x72, 0x69, 0x70, 0x74, 0x69, 0x6f, 0x6e, 0x12, 0x29, 0x0a, 0x10, 0x64, 0x65, 0x73, 0x63, 0x72,
	0x69, 0x70, 0x74, 0x69, 0x6f, 0x6e, 0x5f, 0x68, 0x61, 0x73, 0x68, 0x18, 0x03, 0x20, 0x01, 0x28,
	0x0c, 0x52, 0x0f, 0x64, 0x65, 0x73, 0x63, 0x72, 0x69, 0x70, 0x74, 0x69, 0x6f, 0x6e, 0x48, 0x61,
	0x73, 0x68, 0x12, 0x1f, 0x0a, 0x0b, 0x65, 0x78, 0x70, 0x69, 0x72, 0x79, 0x5f, 0x73, 0x65, 0x63,
	0x73, 0x18, 0x04, 0x20, 0x01, 0x28, 0x03, 0x52, 0x0a, 0x65, 0x78, 0x70, 0x69, 0x72, 0x79, 0x53,
	0x65, 0x63, 0x73, 0x22, 0x6d, 0x0a, 0x12, 0x41, 0x64, 0x64, 0x49, 0x6e, 0x76, 0x6f, 0x69, 0x63,
	0x65, 0x52, 0x65, 0x73, 0x70, 0x6f, 0x6e, 0x73, 0x65, 0x12, 0x27, 0x0a, 0x0f, 0x70, 0x61, 0x79,
	0x6d, 0x65, 0x6e, 0x74, 0x5f, 0x72, 0x65, 0x71, 0x75, 0x65, 0x73, 0x74, 0x18, 0x01, 0x20, 0x01,
	0x28, 0x09, 0x52, 0x0e, 0x70, 0x61, 0x79, 0x6d, 0x65, 0x6e, 0x74, 0x52, 0x65, 0x71, 0x75, 0x65,
	0x73, 0x74, 0x12, 0x1a, 0x0a, 0x08, 0x70, 0x72, 0x65, 0x69, 0x6d, 0x61, 0x67, 0x65, 0x18, 0x02,
	0x20, 0x01, 0x28, 0x0c, 0x52, 0x08, 0x70, 0x72, 0x65, 0x69, 0x6d, 0x61, 0x67, 0x65, 0x12, 0x12,
	0x0a, 0x04, 0x68, 0x61, 0x73, 0x68, 0x18, 0x03, 0x20, 0x01, 0x28, 0x0c, 0x52, 0x04, 0x68, 0x61,
	0x73, 0x68, 0x22, 0x33, 0x0a, 0x1d, 0x53, 0x75, 0x62, 0x73, 0x63, 0x72, 0x69, 0x62, 0x65, 0x53,
	0x69, 0x6e, 0x67, 0x6c, 0x65, 0x49, 0x6e, 0x76, 0x6f, 0x69, 0x63, 0x65, 0x52, 0x65, 0x71, 0x75,
	0x65, 0x73, 0x74, 0x12, 0x12, 0x0a, 0x04, 0x68, 0x61, 0x73, 0x68, 0x18, 0x01, 0x20, 0x01, 0x28,
	0x0c, 0x52, 0x04, 0x68, 0x61, 0x73, 0x68, 0x22, 0xc0, 0x03, 0x0a, 0x1e, 0x53, 0x75, 0x62, 0x73,
	0x63, 0x72, 0x69, 0x62, 0x65, 0x53, 0x69, 0x6e, 0x67, 0x6c, 0x65, 0x49, 0x6e, 0x76, 0x6f, 0x69,
	0x63, 0x65, 0x52, 0x65, 0x73, 0x70, 0x6f, 0x6e, 0x73, 0x65, 0x12, 0x48, 0x0a, 0x05, 0x73, 0x74,
	0x61, 0x74, 0x65, 0x18, 0x01, 0x20, 0x01, 0x28, 0x0e, 0x32, 0x32, 0x2e, 0x6c, 0x6e, 0x6d, 0x75,
	0x78, 0x2e, 0x53, 0x75, 0x62, 0x73, 0x63, 0x72, 0x69, 0x62, 0x65, 0x53, 0x69, 0x6e, 0x67, 0x6c,
	0x65, 0x49, 0x6e, 0x76, 0x6f, 0x69, 0x63, 0x65, 0x52, 0x65, 0x73, 0x70, 0x6f, 0x6e, 0x73, 0x65,
	0x2e, 0x49, 0x6e, 0x76, 0x6f, 0x69, 0x63, 0x65, 0x53, 0x74, 0x61, 0x74, 0x65, 0x52, 0x05, 0x73,
	0x74, 0x61, 0x74, 0x65, 0x12, 0x60, 0x0a, 0x10, 0x63, 0x61, 0x6e, 0x63, 0x65, 0x6c, 0x6c, 0x65,
	0x64, 0x5f, 0x72, 0x65, 0x61, 0x73, 0x6f, 0x6e, 0x18, 0x02, 0x20, 0x01, 0x28, 0x0e, 0x32, 0x35,
	0x2e, 0x6c, 0x6e, 0x6d, 0x75, 0x78, 0x2e, 0x53, 0x75, 0x62, 0x73, 0x63, 0x72, 0x69, 0x62, 0x65,
	0x53, 0x69, 0x6e, 0x67, 0x6c, 0x65, 0x49, 0x6e, 0x76, 0x6f, 0x69, 0x63, 0x65, 0x52, 0x65, 0x73,
	0x70, 0x6f, 0x6e, 0x73, 0x65, 0x2e, 0x43, 0x61, 0x6e, 0x63, 0x65, 0x6c, 0x6c, 0x65, 0x64, 0x52,
	0x65, 0x61, 0x73, 0x6f, 0x6e, 0x52, 0x0f, 0x63, 0x61, 0x6e, 0x63, 0x65, 0x6c, 0x6c, 0x65, 0x64,
	0x52, 0x65, 0x61, 0x73, 0x6f, 0x6e, 0x22, 0x89, 0x01, 0x0a, 0x0c, 0x49, 0x6e, 0x76, 0x6f, 0x69,
	0x63, 0x65, 0x53, 0x74, 0x61, 0x74, 0x65, 0x12, 0x0e, 0x0a, 0x0a, 0x53, 0x54, 0x41, 0x54, 0x45,
	0x5f, 0x4f, 0x50, 0x45, 0x4e, 0x10, 0x00, 0x12, 0x12, 0x0a, 0x0e, 0x53, 0x54, 0x41, 0x54, 0x45,
	0x5f, 0x41, 0x43, 0x43, 0x45, 0x50, 0x54, 0x45, 0x44, 0x10, 0x01, 0x12, 0x1a, 0x0a, 0x16, 0x53,
	0x54, 0x41, 0x54, 0x45, 0x5f, 0x53, 0x45, 0x54, 0x54, 0x4c, 0x45, 0x5f, 0x52, 0x45, 0x51, 0x55,
	0x45, 0x53, 0x54, 0x45, 0x44, 0x10, 0x02, 0x12, 0x11, 0x0a, 0x0d, 0x53, 0x54, 0x41, 0x54, 0x45,
	0x5f, 0x53, 0x45, 0x54, 0x54, 0x4c, 0x45, 0x44, 0x10, 0x03, 0x12, 0x11, 0x0a, 0x0d, 0x53, 0x54,
	0x41, 0x54, 0x45, 0x5f, 0x45, 0x58, 0x50, 0x49, 0x52, 0x45, 0x44, 0x10, 0x04, 0x12, 0x13, 0x0a,
	0x0f, 0x53, 0x54, 0x41, 0x54, 0x45, 0x5f, 0x43, 0x41, 0x4e, 0x43, 0x45, 0x4c, 0x4c, 0x45, 0x44,
	0x10, 0x05, 0x22, 0x66, 0x0a, 0x0f, 0x43, 0x61, 0x6e, 0x63, 0x65, 0x6c, 0x6c, 0x65, 0x64, 0x52,
	0x65, 0x61, 0x73, 0x6f, 0x6e, 0x12, 0x0f, 0x0a, 0x0b, 0x52, 0x45, 0x41, 0x53, 0x4f, 0x4e, 0x5f,
	0x4e, 0x4f, 0x4e, 0x45, 0x10, 0x00, 0x12, 0x12, 0x0a, 0x0e, 0x52, 0x45, 0x41, 0x53, 0x4f, 0x4e,
	0x5f, 0x45, 0x58, 0x50, 0x49, 0x52, 0x45, 0x44, 0x10, 0x01, 0x12, 0x19, 0x0a, 0x15, 0x52, 0x45,
	0x41, 0x53, 0x4f, 0x4e, 0x5f, 0x41, 0x43, 0x43, 0x45, 0x50, 0x54, 0x5f, 0x54, 0x49, 0x4d, 0x45,
	0x4f, 0x55, 0x54, 0x10, 0x02, 0x12, 0x13, 0x0a, 0x0f, 0x52, 0x45, 0x41, 0x53, 0x4f, 0x4e, 0x5f,
	0x45, 0x58, 0x54, 0x45, 0x52, 0x4e, 0x41, 0x4c, 0x10, 0x03, 0x22, 0x2a, 0x0a, 0x14, 0x53, 0x65,
	0x74, 0x74, 0x6c, 0x65, 0x49, 0x6e, 0x76, 0x6f, 0x69, 0x63, 0x65, 0x52, 0x65, 0x71, 0x75, 0x65,
	0x73, 0x74, 0x12, 0x12, 0x0a, 0x04, 0x68, 0x61, 0x73, 0x68, 0x18, 0x01, 0x20, 0x01, 0x28, 0x0c,
	0x52, 0x04, 0x68, 0x61, 0x73, 0x68, 0x22, 0x17, 0x0a, 0x15, 0x53, 0x65, 0x74, 0x74, 0x6c, 0x65,
	0x49, 0x6e, 0x76, 0x6f, 0x69, 0x63, 0x65, 0x52, 0x65, 0x73, 0x70, 0x6f, 0x6e, 0x73, 0x65, 0x22,
	0x2a, 0x0a, 0x14, 0x43, 0x61, 0x6e, 0x63, 0x65, 0x6c, 0x49, 0x6e, 0x76, 0x6f, 0x69, 0x63, 0x65,
	0x52, 0x65, 0x71, 0x75, 0x65, 0x73, 0x74, 0x12, 0x12, 0x0a, 0x04, 0x68, 0x61, 0x73, 0x68, 0x18,
	0x01, 0x20, 0x01, 0x28, 0x0c, 0x52, 0x04, 0x68, 0x61, 0x73, 0x68, 0x22, 0x17, 0x0a, 0x15, 0x43,
	0x61, 0x6e, 0x63, 0x65, 0x6c, 0x49, 0x6e, 0x76, 0x6f, 0x69, 0x63, 0x65, 0x52, 0x65, 0x73, 0x70,
	0x6f, 0x6e, 0x73, 0x65, 0x32, 0xcd, 0x02, 0x0a, 0x07, 0x53, 0x65, 0x72, 0x76, 0x69, 0x63, 0x65,
	0x12, 0x41, 0x0a, 0x0a, 0x41, 0x64, 0x64, 0x49, 0x6e, 0x76, 0x6f, 0x69, 0x63, 0x65, 0x12, 0x18,
	0x2e, 0x6c, 0x6e, 0x6d, 0x75, 0x78, 0x2e, 0x41, 0x64, 0x64, 0x49, 0x6e, 0x76, 0x6f, 0x69, 0x63,
	0x65, 0x52, 0x65, 0x71, 0x75, 0x65, 0x73, 0x74, 0x1a, 0x19, 0x2e, 0x6c, 0x6e, 0x6d, 0x75, 0x78,
	0x2e, 0x41, 0x64, 0x64, 0x49, 0x6e, 0x76, 0x6f, 0x69, 0x63, 0x65, 0x52, 0x65, 0x73, 0x70, 0x6f,
	0x6e, 0x73, 0x65, 0x12, 0x67, 0x0a, 0x16, 0x53, 0x75, 0x62, 0x73, 0x63, 0x72, 0x69, 0x62, 0x65,
	0x53, 0x69, 0x6e, 0x67, 0x6c, 0x65, 0x49, 0x6e, 0x76, 0x6f, 0x69, 0x63, 0x65, 0x12, 0x24, 0x2e,
	0x6c, 0x6e, 0x6d, 0x75, 0x78, 0x2e, 0x53, 0x75, 0x62, 0x73, 0x63, 0x72, 0x69, 0x62, 0x65, 0x53,
	0x69, 0x6e, 0x67, 0x6c, 0x65, 0x49, 0x6e, 0x76, 0x6f, 0x69, 0x63, 0x65, 0x52, 0x65, 0x71, 0x75,
	0x65, 0x73, 0x74, 0x1a, 0x25, 0x2e, 0x6c, 0x6e, 0x6d, 0x75, 0x78, 0x2e, 0x53, 0x75, 0x62, 0x73,
	0x63, 0x72, 0x69, 0x62, 0x65, 0x53, 0x69, 0x6e, 0x67, 0x6c, 0x65, 0x49, 0x6e, 0x76, 0x6f, 0x69,
	0x63, 0x65, 0x52, 0x65, 0x73, 0x70, 0x6f, 0x6e, 0x73, 0x65, 0x30, 0x01, 0x12, 0x4a, 0x0a, 0x0d,
	0x53, 0x65, 0x74, 0x74, 0x6c, 0x65, 0x49, 0x6e, 0x76, 0x6f, 0x69, 0x63, 0x65, 0x12, 0x1b, 0x2e,
	0x6c, 0x6e, 0x6d, 0x75, 0x78, 0x2e, 0x53, 0x65, 0x74, 0x74, 0x6c, 0x65, 0x49, 0x6e, 0x76, 0x6f,
	0x69, 0x63, 0x65, 0x52, 0x65, 0x71, 0x75, 0x65, 0x73, 0x74, 0x1a, 0x1c, 0x2e, 0x6c, 0x6e, 0x6d,
	0x75, 0x78, 0x2e, 0x53, 0x65, 0x74, 0x74, 0x6c, 0x65, 0x49, 0x6e, 0x76, 0x6f, 0x69, 0x63, 0x65,
	0x52, 0x65, 0x73, 0x70, 0x6f, 0x6e, 0x73, 0x65, 0x12, 0x4a, 0x0a, 0x0d, 0x43, 0x61, 0x6e, 0x63,
	0x65, 0x6c, 0x49, 0x6e, 0x76, 0x6f, 0x69, 0x63, 0x65, 0x12, 0x1b, 0x2e, 0x6c, 0x6e, 0x6d, 0x75,
	0x78, 0x2e, 0x43, 0x61, 0x6e, 0x63, 0x65, 0x6c, 0x49, 0x6e, 0x76, 0x6f, 0x69, 0x63, 0x65, 0x52,
	0x65, 0x71, 0x75, 0x65, 0x73, 0x74, 0x1a, 0x1c, 0x2e, 0x6c, 0x6e, 0x6d, 0x75, 0x78, 0x2e, 0x43,
	0x61, 0x6e, 0x63, 0x65, 0x6c, 0x49, 0x6e, 0x76, 0x6f, 0x69, 0x63, 0x65, 0x52, 0x65, 0x73, 0x70,
	0x6f, 0x6e, 0x73, 0x65, 0x42, 0x22, 0x5a, 0x20, 0x67, 0x69, 0x74, 0x68, 0x75, 0x62, 0x2e, 0x63,
	0x6f, 0x6d, 0x2f, 0x62, 0x6f, 0x74, 0x74, 0x6c, 0x65, 0x70, 0x61, 0x79, 0x2f, 0x6c, 0x6e, 0x6d,
	0x75, 0x78, 0x5f, 0x70, 0x72, 0x6f, 0x74, 0x6f, 0x62, 0x06, 0x70, 0x72, 0x6f, 0x74, 0x6f, 0x33,
}

var (
	file_lnmux_proto_rawDescOnce sync.Once
	file_lnmux_proto_rawDescData = file_lnmux_proto_rawDesc
)

func file_lnmux_proto_rawDescGZIP() []byte {
	file_lnmux_proto_rawDescOnce.Do(func() {
		file_lnmux_proto_rawDescData = protoimpl.X.CompressGZIP(file_lnmux_proto_rawDescData)
	})
	return file_lnmux_proto_rawDescData
}

var file_lnmux_proto_enumTypes = make([]protoimpl.EnumInfo, 2)
var file_lnmux_proto_msgTypes = make([]protoimpl.MessageInfo, 8)
var file_lnmux_proto_goTypes = []interface{}{
	(SubscribeSingleInvoiceResponse_InvoiceState)(0),    // 0: lnmux.SubscribeSingleInvoiceResponse.InvoiceState
	(SubscribeSingleInvoiceResponse_CancelledReason)(0), // 1: lnmux.SubscribeSingleInvoiceResponse.CancelledReason
	(*AddInvoiceRequest)(nil),                           // 2: lnmux.AddInvoiceRequest
	(*AddInvoiceResponse)(nil),                          // 3: lnmux.AddInvoiceResponse
	(*SubscribeSingleInvoiceRequest)(nil),               // 4: lnmux.SubscribeSingleInvoiceRequest
	(*SubscribeSingleInvoiceResponse)(nil),              // 5: lnmux.SubscribeSingleInvoiceResponse
	(*SettleInvoiceRequest)(nil),                        // 6: lnmux.SettleInvoiceRequest
	(*SettleInvoiceResponse)(nil),                       // 7: lnmux.SettleInvoiceResponse
	(*CancelInvoiceRequest)(nil),                        // 8: lnmux.CancelInvoiceRequest
	(*CancelInvoiceResponse)(nil),                       // 9: lnmux.CancelInvoiceResponse
}
var file_lnmux_proto_depIdxs = []int32{
	0, // 0: lnmux.SubscribeSingleInvoiceResponse.state:type_name -> lnmux.SubscribeSingleInvoiceResponse.InvoiceState
	1, // 1: lnmux.SubscribeSingleInvoiceResponse.cancelled_reason:type_name -> lnmux.SubscribeSingleInvoiceResponse.CancelledReason
	2, // 2: lnmux.Service.AddInvoice:input_type -> lnmux.AddInvoiceRequest
	4, // 3: lnmux.Service.SubscribeSingleInvoice:input_type -> lnmux.SubscribeSingleInvoiceRequest
	6, // 4: lnmux.Service.SettleInvoice:input_type -> lnmux.SettleInvoiceRequest
	8, // 5: lnmux.Service.CancelInvoice:input_type -> lnmux.CancelInvoiceRequest
	3, // 6: lnmux.Service.AddInvoice:output_type -> lnmux.AddInvoiceResponse
	5, // 7: lnmux.Service.SubscribeSingleInvoice:output_type -> lnmux.SubscribeSingleInvoiceResponse
	7, // 8: lnmux.Service.SettleInvoice:output_type -> lnmux.SettleInvoiceResponse
	9, // 9: lnmux.Service.CancelInvoice:output_type -> lnmux.CancelInvoiceResponse
	6, // [6:10] is the sub-list for method output_type
	2, // [2:6] is the sub-list for method input_type
	2, // [2:2] is the sub-list for extension type_name
	2, // [2:2] is the sub-list for extension extendee
	0, // [0:2] is the sub-list for field type_name
}

func init() { file_lnmux_proto_init() }
func file_lnmux_proto_init() {
	if File_lnmux_proto != nil {
		return
	}
	if !protoimpl.UnsafeEnabled {
		file_lnmux_proto_msgTypes[0].Exporter = func(v interface{}, i int) interface{} {
			switch v := v.(*AddInvoiceRequest); i {
			case 0:
				return &v.state
			case 1:
				return &v.sizeCache
			case 2:
				return &v.unknownFields
			default:
				return nil
			}
		}
		file_lnmux_proto_msgTypes[1].Exporter = func(v interface{}, i int) interface{} {
			switch v := v.(*AddInvoiceResponse); i {
			case 0:
				return &v.state
			case 1:
				return &v.sizeCache
			case 2:
				return &v.unknownFields
			default:
				return nil
			}
		}
		file_lnmux_proto_msgTypes[2].Exporter = func(v interface{}, i int) interface{} {
			switch v := v.(*SubscribeSingleInvoiceRequest); i {
			case 0:
				return &v.state
			case 1:
				return &v.sizeCache
			case 2:
				return &v.unknownFields
			default:
				return nil
			}
		}
		file_lnmux_proto_msgTypes[3].Exporter = func(v interface{}, i int) interface{} {
			switch v := v.(*SubscribeSingleInvoiceResponse); i {
			case 0:
				return &v.state
			case 1:
				return &v.sizeCache
			case 2:
				return &v.unknownFields
			default:
				return nil
			}
		}
		file_lnmux_proto_msgTypes[4].Exporter = func(v interface{}, i int) interface{} {
			switch v := v.(*SettleInvoiceRequest); i {
			case 0:
				return &v.state
			case 1:
				return &v.sizeCache
			case 2:
				return &v.unknownFields
			default:
				return nil
			}
		}
		file_lnmux_proto_msgTypes[5].Exporter = func(v interface{}, i int) interface{} {
			switch v := v.(*SettleInvoiceResponse); i {
			case 0:
				return &v.state
			case 1:
				return &v.sizeCache
			case 2:
				return &v.unknownFields
			default:
				return nil
			}
		}
		file_lnmux_proto_msgTypes[6].Exporter = func(v interface{}, i int) interface{} {
			switch v := v.(*CancelInvoiceRequest); i {
			case 0:
				return &v.state
			case 1:
				return &v.sizeCache
			case 2:
				return &v.unknownFields
			default:
				return nil
			}
		}
		file_lnmux_proto_msgTypes[7].Exporter = func(v interface{}, i int) interface{} {
			switch v := v.(*CancelInvoiceResponse); i {
			case 0:
				return &v.state
			case 1:
				return &v.sizeCache
			case 2:
				return &v.unknownFields
			default:
				return nil
			}
		}
	}
	type x struct{}
	out := protoimpl.TypeBuilder{
		File: protoimpl.DescBuilder{
			GoPackagePath: reflect.TypeOf(x{}).PkgPath(),
			RawDescriptor: file_lnmux_proto_rawDesc,
			NumEnums:      2,
			NumMessages:   8,
			NumExtensions: 0,
			NumServices:   1,
		},
		GoTypes:           file_lnmux_proto_goTypes,
		DependencyIndexes: file_lnmux_proto_depIdxs,
		EnumInfos:         file_lnmux_proto_enumTypes,
		MessageInfos:      file_lnmux_proto_msgTypes,
	}.Build()
	File_lnmux_proto = out.File
	file_lnmux_proto_rawDesc = nil
	file_lnmux_proto_goTypes = nil
	file_lnmux_proto_depIdxs = nil
}