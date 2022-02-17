// Package bnk implements access to the Wwise SoundBank file format.
package bnk

import (
	"encoding/binary"
	"fmt"
	"io"
	"strings"
)

import (
	"util"
	"wwise"
)

// The number of bytes used to describe the header of a section.
const SECTION_HEADER_BYTES = 8

// The number of bytes used to describe the known portion of a BKHD section,
// excluding its own header.
const BKHD_SECTION_BYTES = 8

// The number of bytes used to describe a single data index
// entry (a WemDescriptor) within the DIDX section.
const DIDX_ENTRY_BYTES = 12

// The number of bytes used to describe the count of objects in the HIRC section.
const OBJECT_COUNT_BYTES = 4

// The identifier for the start of the BKHD (Bank Header) section.
var bkhdHeaderId = [4]byte{'B', 'K', 'H', 'D'}

// The identifier for the start of the DIDX (Data Index) section.
var didxHeaderId = [4]byte{'D', 'I', 'D', 'X'}

// The identifier for the start of the DATA section.
var dataHeaderId = [4]byte{'D', 'A', 'T', 'A'}

// The identifier for the start of the HIRC section.
var hircHeaderId = [4]byte{'H', 'I', 'R', 'C'}

// Section represents a single section of a Wwise SoundBank.
type Section interface {
	io.WriterTo
	fmt.Stringer
}

// A SectionHeader represents a single Wwise SoundBank header.
type SectionHeader struct {
	Identifier [4]byte
	Length     uint32
}

// A BankHeaderSection represents the BKHD section of a SoundBank file.
type BankHeaderSection struct {
	Header          *SectionHeader
	Descriptor      BankDescriptor
	RemainingReader io.Reader
}

// A BankDescriptor provides metadata about the overall SoundBank file.
type BankDescriptor struct {
	Version uint32
	BankId  uint32
}

// A DataIndexSection represents the DIDX section of a SoundBank file.
type DataIndexSection struct {
	Header *SectionHeader
	// The count of wems in this SoundBank.
	WemCount int
	// A list of all wem IDs, in order of their offset into the file.
	WemIds []uint32
	// A mapping from wem ID to its descriptor.
	DescriptorMap map[uint32]*wwise.WemDescriptor
}

// A DataIndexSection represents the DATA section of a SoundBank file.
type DataSection struct {
	Header *SectionHeader
	// The offset into the file where the data portion of the DATA section begins.
	// This is the location where wem entries are stored.
	DataStart uint32
	Wems      []*wwise.Wem
}

// A ObjectHierarchySection represents the HIRC section of a SoundBank file,
// which contains all wwise metadata objects defining the behavior and
// properties of wems.
type ObjectHierarchySection struct {
	Header      *SectionHeader
	ObjectCount uint32
	objects     []Object
	// A convenience field for accessing the loop parameters of every wem. It maps
	// the wem id of the loop in question to the loop value, where 0 represents
	// infinity.
	loopOf      map[uint32]uint32
	wemToObject map[uint32]*SfxVoiceSoundObject
}

// An UnknownSection represents an unknown section in a SoundBank file.
type UnknownSection struct {
	Header *SectionHeader
	// A reader to read the data of this section.
	Reader io.Reader
}

// NewBankHeaderSection creates a new BankHeaderSection, reading from sr, which
// must be seeked to the start of the BKHD section data.
// It is an error to call this method on a non-BKHD header.
func (hdr *SectionHeader) NewBankHeaderSection(sr util.ReadSeekerAt) (*BankHeaderSection, error) {
	if hdr.Identifier != bkhdHeaderId {
		panic(fmt.Sprintf("Expected BKHD header but got: %s", hdr.Identifier))
	}
	sec := new(BankHeaderSection)
	sec.Header = hdr
	desc := BankDescriptor{}
	err := binary.Read(sr, binary.LittleEndian, &desc)
	if err != nil {
		return nil, err
	}
	sec.Descriptor = desc
	// Get the offset into the file where the known portion of the BKHD ends.
	knownOffset, _ := sr.Seek(0, io.SeekCurrent)
	remaining := int64(hdr.Length - BKHD_SECTION_BYTES)
	sec.RemainingReader = util.NewResettingReader(sr, knownOffset, remaining)
	sr.Seek(remaining, io.SeekCurrent)

	return sec, nil
}

// WriteTo writes the full contents of this BankHeaderSection to the Writer
// specified by w.
func (hdr *BankHeaderSection) WriteTo(w io.Writer) (written int64, err error) {
	err = binary.Write(w, binary.LittleEndian, hdr.Header)
	if err != nil {
		return
	}
	written = int64(SECTION_HEADER_BYTES)
	err = binary.Write(w, binary.LittleEndian, hdr.Descriptor)
	if err != nil {
		return
	}
	written += int64(BKHD_SECTION_BYTES)
	n, err := io.Copy(w, hdr.RemainingReader)
	if err != nil {
		return
	}
	written += int64(n)
	return written, nil
}

func (hdr *BankHeaderSection) String() string {
	return fmt.Sprintf("%s: len(%d) version(%d) id(%d)\n",
		hdr.Header.Identifier, hdr.Header.Length, hdr.Descriptor.Version,
		hdr.Descriptor.BankId)
}

// NewDataIndexSection creates a new DataIndexSection, reading from r, which must
// be seeked to the start of the DIDX section data.
// It is an error to call this method on a non-DIDX header.
func (hdr *SectionHeader) NewDataIndexSection(r io.Reader) (*DataIndexSection, error) {
	if hdr.Identifier != didxHeaderId {
		panic(fmt.Sprintf("Expected DIDX header but got: %s", hdr.Identifier))
	}
	wemCount := int(hdr.Length / DIDX_ENTRY_BYTES)
	sec := DataIndexSection{hdr, wemCount, make([]uint32, 0),
		make(map[uint32]*wwise.WemDescriptor)}
	for i := 0; i < wemCount; i++ {
		var desc wwise.WemDescriptor
		err := binary.Read(r, binary.LittleEndian, &desc)
		if err != nil {
			return nil, err
		}

		if _, ok := sec.DescriptorMap[desc.WemId]; ok {
			panic(fmt.Sprintf(
				"%d is an illegal repeated wem ID in the DIDX", desc.WemId))
		}
		sec.WemIds = append(sec.WemIds, desc.WemId)
		sec.DescriptorMap[desc.WemId] = &desc
	}

	return &sec, nil
}

// WriteTo writes the full contents of this DataIndexSection to the Writer
// specified by w.
func (idx *DataIndexSection) WriteTo(w io.Writer) (written int64, err error) {
	err = binary.Write(w, binary.LittleEndian, idx.Header)
	if err != nil {
		return
	}
	written = int64(SECTION_HEADER_BYTES)

	for _, id := range idx.WemIds {
		desc := idx.DescriptorMap[id]
		err = binary.Write(w, binary.LittleEndian, desc)
		if err != nil {
			return
		}
		written += int64(DIDX_ENTRY_BYTES)
	}
	return written, nil
}

func (idx *DataIndexSection) String() string {
	b := new(strings.Builder)
	total := uint32(0)
	for _, desc := range idx.DescriptorMap {
		total += desc.Length
	}
	fmt.Fprintf(b, "%s: len(%d) wem_count(%d)\n", idx.Header.Identifier,
		idx.Header.Length, idx.WemCount)
	fmt.Fprintf(b, "DIDX: WEM total size: %d\n", total)
	return b.String()
}

// NewDataSection creates a new DataSection, reading from sr, which must be
// seeked to the start of the DATA section data. idx specifies how each wem
// should be indexed from, given the current sr offset.
// It is an error to call this method on a non-DATA header.
func (hdr *SectionHeader) NewDataSection(sr util.ReadSeekerAt,
	idx *DataIndexSection) (*DataSection, error) {
	if hdr.Identifier != dataHeaderId {
		panic(fmt.Sprintf("Expected DATA header but got: %s", hdr.Identifier))
	}
	dataOffset, _ := sr.Seek(0, io.SeekCurrent)

	sec := DataSection{hdr, uint32(dataOffset), make([]*wwise.Wem, 0)}
	for i, id := range idx.WemIds {
		desc := idx.DescriptorMap[id]
		wemStartOffset := dataOffset + int64(desc.Offset)
		wemReader := util.NewResettingReader(sr, wemStartOffset, int64(desc.Length))

		var padding util.ReadSeekerAt

		if i <= len(idx.WemIds)-1 {
			wemEndOffset := wemStartOffset + int64(desc.Length)
			var nextOffset int64
			if i == len(idx.WemIds)-1 {
				// This is the last wem, check how many bytes remain until the end of
				// the data section.
				nextOffset = dataOffset + int64(hdr.Length)
			} else {
				// This is not the last wem, check how many bytes remain until the next
				// wem.
				nextDesc := idx.DescriptorMap[idx.WemIds[i+1]]
				nextOffset = dataOffset + int64(nextDesc.Offset)
			}
			remaining := nextOffset - wemEndOffset
			// Pass a Reader over the remaining section if we have remaining bytes to
			// read, or an empty Reader if remaining is 0 (no bytes will be read).
			padding = util.NewResettingReader(sr, wemEndOffset, remaining)
		}

		wem := wwise.Wem{wemReader, desc, padding}
		sec.Wems = append(sec.Wems, &wem)
	}

	sr.Seek(int64(hdr.Length), io.SeekCurrent)
	return &sec, nil
}

// WriteTo writes the full contents of this DataSection to the Writer specified
// by w.
func (data *DataSection) WriteTo(w io.Writer) (written int64, err error) {
	err = binary.Write(w, binary.LittleEndian, data.Header)
	if err != nil {
		return
	}
	written = int64(SECTION_HEADER_BYTES)
	for _, wem := range data.Wems {
		n, err := io.Copy(w, wem)
		if err != nil {
			return written, err
		}
		written += int64(n)
		n, err = io.Copy(w, wem.Padding)
		if err != nil {
			return written, err
		}
		written += int64(n)
	}

	return written, nil
}

func (data *DataSection) String() string {
	return fmt.Sprintf("%s: len(%d)\n", data.Header.Identifier, data.Header.Length)
}

// NewObjectHierarchySection creates a new ObjectHierarchySection, reading from
// sr, which must be seeked to the start of the HIRC section data.
// It is an error to call this method on a non-HIRC header.
func (hdr *SectionHeader) NewObjectHierarchySection(sr util.ReadSeekerAt) (*ObjectHierarchySection, error) {
	if hdr.Identifier != hircHeaderId {
		panic(fmt.Sprintf("Expected HIRC header but got: %s", hdr.Identifier))
	}
	sec := new(ObjectHierarchySection)
	sec.Header = hdr
	sec.loopOf = make(map[uint32]uint32)
	sec.wemToObject = make(map[uint32]*SfxVoiceSoundObject)

	var count uint32
	err := binary.Read(sr, binary.LittleEndian, &count)
	if err != nil {
		return nil, err
	}
	sec.ObjectCount = count

	for i := uint32(0); i < sec.ObjectCount; i++ {
		desc := new(ObjectDescriptor)
		err := binary.Read(sr, binary.LittleEndian, desc)
		if err != nil {
			return nil, err
		}
		switch id := desc.Type; id {
		case soundObjectId:
			obj, err := desc.NewSfxVoiceSoundObject(sr)
			if err != nil {
				return nil, err
			}

			sec.wemToObject[obj.WemDescriptor.WemId] = obj
			if obj.Structure.loops {
				sec.loopOf[obj.WemDescriptor.WemId] = obj.Structure.loopCount
			}
			sec.objects = append(sec.objects, obj)
		default:
			obj, err := desc.NewUnknownObject(sr)
			if err != nil {
				return nil, err
			}
			sec.objects = append(sec.objects, obj)
		}
	}

	return sec, nil
}

// WriteTo writes the full contents of this ObjectHierarchySection to the Writer
// specified by w.
func (hrc *ObjectHierarchySection) WriteTo(w io.Writer) (written int64, err error) {
	err = binary.Write(w, binary.LittleEndian, hrc.Header)
	if err != nil {
		return
	}
	written = int64(SECTION_HEADER_BYTES)

	err = binary.Write(w, binary.LittleEndian, hrc.ObjectCount)
	if err != nil {
		return
	}
	written += int64(OBJECT_COUNT_BYTES)

	for _, obj := range hrc.objects {
		n, err := obj.WriteTo(w)
		if err != nil {
			return written, err
		}
		written += int64(n)
	}
	return written, nil
}

func (hrc *ObjectHierarchySection) String() string {
	b := new(strings.Builder)

	fmt.Fprintf(b, "%s: len(%d) object_count(%d) \n",
		hrc.Header.Identifier, hrc.Header.Length, hrc.ObjectCount)
	return b.String()
}

// NewUnknownSection creates a new UnknownSection, reading from sr, which
// must be seeked to the start of the unknown section data.
func (hdr *SectionHeader) NewUnknownSection(sr util.ReadSeekerAt) (*UnknownSection, error) {
	// Get the offset into the file where the data portion of this section begins.
	dataOffset, _ := sr.Seek(0, io.SeekCurrent)
	r := util.NewResettingReader(sr, dataOffset, int64(hdr.Length))
	sr.Seek(int64(hdr.Length), io.SeekCurrent)
	return &UnknownSection{hdr, r}, nil
}

// WriteTo writes the full contents of this UnknownSection to the Writer
// specified by w.
func (unknown *UnknownSection) WriteTo(w io.Writer) (written int64, err error) {
	err = binary.Write(w, binary.LittleEndian, unknown.Header)
	if err != nil {
		return
	}
	written = int64(SECTION_HEADER_BYTES)

	n, err := io.Copy(w, unknown.Reader)
	if err != nil {
		return written, err
	}
	written += int64(n)

	return written, nil
}

func (unknown *UnknownSection) String() string {
	return fmt.Sprintf("%s: len(%d)\n", unknown.Header.Identifier,
		unknown.Header.Length)
}
