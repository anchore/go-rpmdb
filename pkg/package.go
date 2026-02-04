package rpmdb

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"path"
	"strings"
	"time"

	"golang.org/x/xerrors"
)

type PackageInfo struct {
	Epoch           *int
	Name            string
	Version         string
	Release         string
	Arch            string
	SourceRpm       string
	Size            int
	License         string
	Vendor          string
	Modularitylabel string
	Summary         string
	PGP             string
	SigMD5          string
	RSAHeader       string
	DigestAlgorithm DigestAlgorithm
	InstallTime     int
	BaseNames       []string
	DirIndexes      []int32
	DirNames        []string
	FileSizes       []int32
	FileDigests     []string
	FileModes       []uint16
	FileFlags       []int32
	UserNames       []string
	GroupNames      []string

	Provides []string
	Requires []string
}

type FileInfo struct {
	Path      string
	Mode      uint16
	Digest    string
	Size      int32
	Username  string
	Groupname string
	Flags     FileFlags
}

// ref. https://github.com/rpm-software-management/rpm/blob/rpm-4.14.3-release/lib/tagexts.c#L752
func getNEVRA(indexEntries []indexEntry) (*PackageInfo, error) {
	pkgInfo := &PackageInfo{}
	for _, ie := range indexEntries {
		switch ie.Info.Tag {
		case RPMTAG_DIRINDEXES:
			if ie.Info.Type != RPM_INT32_TYPE {
				return nil, xerrors.New("invalid tag dir indexes")
			}

			dirIndexes, err := parseInt32Array(ie.Data, ie.Length)
			if err != nil {
				return nil, xerrors.Errorf("unable to read dir indexes: %w", err)
			}
			pkgInfo.DirIndexes = dirIndexes
		case RPMTAG_DIRNAMES:
			if ie.Info.Type != RPM_STRING_ARRAY_TYPE {
				return nil, xerrors.New("invalid tag dir names")
			}
			pkgInfo.DirNames = parseStringArray(ie.Data)
		case RPMTAG_BASENAMES:
			if ie.Info.Type != RPM_STRING_ARRAY_TYPE {
				return nil, xerrors.New("invalid tag base names")
			}
			pkgInfo.BaseNames = parseStringArray(ie.Data)
		case RPMTAG_MODULARITYLABEL:
			if ie.Info.Type != RPM_STRING_TYPE {
				return nil, xerrors.New("invalid tag modularitylabel")
			}
			pkgInfo.Modularitylabel = string(bytes.TrimRight(ie.Data, "\x00"))
		case RPMTAG_NAME:
			if ie.Info.Type != RPM_STRING_TYPE {
				return nil, xerrors.New("invalid tag name")
			}
			pkgInfo.Name = string(bytes.TrimRight(ie.Data, "\x00"))
		case RPMTAG_EPOCH:
			if ie.Info.Type != RPM_INT32_TYPE {
				return nil, xerrors.New("invalid tag epoch")
			}

			if ie.Data != nil {
				value, err := parseInt32(ie.Data)
				if err != nil {
					return nil, xerrors.Errorf("failed to parse epoch: %w", err)
				}
				pkgInfo.Epoch = &value
			}
		case RPMTAG_VERSION:
			if ie.Info.Type != RPM_STRING_TYPE {
				return nil, xerrors.New("invalid tag version")
			}
			pkgInfo.Version = string(bytes.TrimRight(ie.Data, "\x00"))
		case RPMTAG_RELEASE:
			if ie.Info.Type != RPM_STRING_TYPE {
				return nil, xerrors.New("invalid tag release")
			}
			pkgInfo.Release = string(bytes.TrimRight(ie.Data, "\x00"))
		case RPMTAG_ARCH:
			if ie.Info.Type != RPM_STRING_TYPE {
				return nil, xerrors.New("invalid tag arch")
			}
			pkgInfo.Arch = string(bytes.TrimRight(ie.Data, "\x00"))
		case RPMTAG_SOURCERPM:
			if ie.Info.Type != RPM_STRING_TYPE {
				return nil, xerrors.New("invalid tag sourcerpm")
			}
			pkgInfo.SourceRpm = string(bytes.TrimRight(ie.Data, "\x00"))
			if pkgInfo.SourceRpm == "(none)" {
				pkgInfo.SourceRpm = ""
			}
		case RPMTAG_PROVIDENAME:
			if ie.Info.Type != RPM_STRING_ARRAY_TYPE {
				return nil, xerrors.New("invalid tag providename")
			}
			pkgInfo.Provides = parseStringArray(ie.Data)
		case RPMTAG_REQUIRENAME:
			if ie.Info.Type != RPM_STRING_ARRAY_TYPE {
				return nil, xerrors.New("invalid tag requirename")
			}
			pkgInfo.Requires = parseStringArray(ie.Data)
		case RPMTAG_LICENSE:
			if ie.Info.Type != RPM_STRING_TYPE {
				return nil, xerrors.New("invalid tag license")
			}
			pkgInfo.License = string(bytes.TrimRight(ie.Data, "\x00"))
			if pkgInfo.License == "(none)" {
				pkgInfo.License = ""
			}
		case RPMTAG_VENDOR:
			if ie.Info.Type != RPM_STRING_TYPE {
				return nil, xerrors.New("invalid tag vendor")
			}
			pkgInfo.Vendor = string(bytes.TrimRight(ie.Data, "\x00"))
			if pkgInfo.Vendor == "(none)" {
				pkgInfo.Vendor = ""
			}
		case RPMTAG_SIZE:
			if ie.Info.Type != RPM_INT32_TYPE {
				return nil, xerrors.New("invalid tag size")
			}

			size, err := parseInt32(ie.Data)
			if err != nil {
				return nil, xerrors.Errorf("failed to parse size: %w", err)
			}
			pkgInfo.Size = size
		case RPMTAG_FILEDIGESTALGO:
			// note: all digests within a package entry only supports a single digest algorithm (there may be future support for
			// algorithm noted for each file entry, but currently unimplemented: https://github.com/rpm-software-management/rpm/blob/0b75075a8d006c8f792d33a57eae7da6b66a4591/lib/rpmtag.h#L256)
			if ie.Info.Type != RPM_INT32_TYPE {
				return nil, xerrors.New("invalid tag digest algo")
			}

			digestAlgorithm, err := parseInt32(ie.Data)
			if err != nil {
				return nil, xerrors.Errorf("failed to parse digest algo: %w", err)
			}

			pkgInfo.DigestAlgorithm = DigestAlgorithm(digestAlgorithm)
		case RPMTAG_FILESIZES:
			// note: there is no distinction between int32, uint32, and []uint32
			if ie.Info.Type != RPM_INT32_TYPE {
				return nil, xerrors.New("invalid tag file-sizes")
			}
			fileSizes, err := parseInt32Array(ie.Data, ie.Length)
			if err != nil {
				return nil, xerrors.Errorf("failed to parse file-sizes: %w", err)
			}
			pkgInfo.FileSizes = fileSizes
		case RPMTAG_FILEDIGESTS:
			if ie.Info.Type != RPM_STRING_ARRAY_TYPE {
				return nil, xerrors.New("invalid tag file-digests")
			}
			pkgInfo.FileDigests = parseStringArray(ie.Data)
		case RPMTAG_FILEMODES:
			// note: there is no distinction between int16, uint16, and []uint16
			if ie.Info.Type != RPM_INT16_TYPE {
				return nil, xerrors.New("invalid tag file-modes")
			}
			fileModes, err := uint16Array(ie.Data, ie.Length)
			if err != nil {
				return nil, xerrors.Errorf("failed to parse file-modes: %w", err)
			}
			pkgInfo.FileModes = fileModes
		case RPMTAG_FILEFLAGS:
			// note: there is no distinction between int32, uint32, and []uint32
			if ie.Info.Type != RPM_INT32_TYPE {
				return nil, xerrors.New("invalid tag file-flags")
			}
			fileFlags, err := parseInt32Array(ie.Data, ie.Length)
			if err != nil {
				return nil, xerrors.Errorf("failed to parse file-flags: %w", err)
			}
			pkgInfo.FileFlags = fileFlags
		case RPMTAG_FILEUSERNAME:
			if ie.Info.Type != RPM_STRING_ARRAY_TYPE {
				return nil, xerrors.New("invalid tag usernames")
			}
			pkgInfo.UserNames = parseStringArray(ie.Data)
		case RPMTAG_FILEGROUPNAME:
			if ie.Info.Type != RPM_STRING_ARRAY_TYPE {
				return nil, xerrors.New("invalid tag groupnames")
			}
			pkgInfo.GroupNames = parseStringArray(ie.Data)
		case RPMTAG_SUMMARY:
			// some libraries have a string value instead of international string, so accounting for both
			if ie.Info.Type != RPM_I18NSTRING_TYPE && ie.Info.Type != RPM_STRING_TYPE {
				return nil, xerrors.New("invalid tag summary")
			}
			// since this is an international string, getting the first null terminated string
			pkgInfo.Summary = string(bytes.Split(ie.Data, []byte{0})[0])
		case RPMTAG_INSTALLTIME:
			if ie.Info.Type != RPM_INT32_TYPE {
				return nil, xerrors.New("invalid tag installtime")
			}
			installTime, err := parseInt32(ie.Data)
			if err != nil {
				return nil, xerrors.Errorf("failed to parse installtime: %w", err)
			}
			pkgInfo.InstallTime = installTime
		case RPMTAG_SIGMD5:
			// It is just string that we need to encode to hex
			digest := ie.Data
			pkgInfo.SigMD5 = hex.EncodeToString(digest)
		case RPMTAG_RSAHEADER:
			if ie.Info.Type != RPM_BIN_TYPE {
				return nil, xerrors.New("invalid rsa signature")
			}
			val, err := parsePGP(ie)
			if err != nil {
				return nil, xerrors.Errorf("failed to parse rsa signature: %w", err)
			}
			pkgInfo.RSAHeader = val
		case RPMTAG_PGP:
			if ie.Info.Type != RPM_BIN_TYPE {
				return nil, xerrors.New("invalid pgp signature")
			}
			val, err := parsePGP(ie)
			if err != nil {
				return nil, xerrors.Errorf("failed to parse pgp signature: %w", err)
			}
			pkgInfo.PGP = val
		}
	}

	return pkgInfo, nil
}

type pgpSig struct {
	_          [3]byte
	Date       int32
	KeyID      [8]byte
	PubKeyAlgo uint8
	HashAlgo   uint8
}

type textSig struct {
	_          [2]byte
	PubKeyAlgo uint8
	HashAlgo   uint8
	_          [4]byte
	Date       int32
	_          [4]byte
	KeyID      [8]byte
}

// OpenPGP signature subpacket types (RFC 4880, Section 5.2.3.1)
// Reference: https://www.rfc-editor.org/rfc/rfc4880#section-5.2.3.1
const (
	// pgpSubpacketSignatureCreationTime contains a 4-octet time field (MUST be in hashed area)
	pgpSubpacketSignatureCreationTime = 2
	// pgpSubpacketIssuerKeyID contains an 8-octet Key ID of the key issuing the signature
	pgpSubpacketIssuerKeyID = 16
)

var pubKeyLookup = map[uint8]string{
	0x01: "RSA",
}
var hashLookup = map[uint8]string{
	0x02: "SHA1",
	0x08: "SHA256",
}

func parsePGP(ie indexEntry) (string, error) {
	var tag, signatureType, version uint8

	r := bytes.NewReader(ie.Data)
	err := binary.Read(r, binary.BigEndian, &tag)
	if err != nil {
		return "", err
	}
	err = binary.Read(r, binary.BigEndian, &signatureType)
	if err != nil {
		return "", err
	}
	err = binary.Read(r, binary.BigEndian, &version)
	if err != nil {
		return "", err
	}

	switch signatureType {
	case 0x01:
		switch version {
		case 0x1c:
			sig := textSig{}
			err = binary.Read(r, binary.BigEndian, &sig)
			if err != nil {
				return "", fmt.Errorf("invalid PGP signature on decode: %w", err)
			}

			pubKeyAlgo := pubKeyLookup[sig.PubKeyAlgo]
			hashAlgo := hashLookup[sig.HashAlgo]
			pkgDate := time.Unix(int64(sig.Date), 0).UTC().Format("Mon Jan _2 15:04:05 2006")
			keyId := sig.KeyID

			return fmt.Sprintf("%s/%s, %s, Key ID %x", pubKeyAlgo, hashAlgo, pkgDate, keyId), nil
		default:
			return decodePGPSig(version, r)
		}
	case 0x02:
		return decodePGPSig(version, r)
	}

	return "", nil
}

func decodePGPSig(version uint8, r io.Reader) (string, error) {
	var pubKeyAlgo, hashAlgo, pkgDate string
	var keyId [8]byte

	switch {
	case version > 0x15:
		// For version > 0x15, the next byte is the actual OpenPGP v4 signature version,
		// followed by the v4 signature packet body.
		//
		// OpenPGP v4 signature packet format (RFC 4880, Section 5.2.3):
		// Reference: https://www.rfc-editor.org/rfc/rfc4880#section-5.2.3
		//
		// - 1 byte: version (0x04)
		// - 1 byte: signature type
		// - 1 byte: public-key algorithm
		// - 1 byte: hash algorithm
		// - 2 bytes: hashed subpacket data length
		// - N bytes: hashed subpacket data (contains signature creation time, subpacket type 2)
		// - 2 bytes: unhashed subpacket data length
		// - M bytes: unhashed subpacket data (contains issuer key ID, subpacket type 16)
		// - 2 bytes: left 16 bits of signed hash value
		// - signature MPI(s)

		var v4Version, sigType, pubKeyAlgoByte, hashAlgoByte uint8
		if err := binary.Read(r, binary.BigEndian, &v4Version); err != nil {
			return "", fmt.Errorf("failed to read v4 version: %w", err)
		}
		if err := binary.Read(r, binary.BigEndian, &sigType); err != nil {
			return "", fmt.Errorf("failed to read signature type: %w", err)
		}
		if err := binary.Read(r, binary.BigEndian, &pubKeyAlgoByte); err != nil {
			return "", fmt.Errorf("failed to read public key algorithm: %w", err)
		}
		if err := binary.Read(r, binary.BigEndian, &hashAlgoByte); err != nil {
			return "", fmt.Errorf("failed to read hash algorithm: %w", err)
		}
		pubKeyAlgo = pubKeyLookup[pubKeyAlgoByte]
		hashAlgo = hashLookup[hashAlgoByte]

		// Read hashed subpacket length and data
		var hashedLen uint16
		if err := binary.Read(r, binary.BigEndian, &hashedLen); err != nil {
			return "", fmt.Errorf("failed to read hashed subpacket length: %w", err)
		}
		hashedData := make([]byte, hashedLen)
		if _, err := io.ReadFull(r, hashedData); err != nil {
			return "", fmt.Errorf("failed to read hashed subpackets: %w", err)
		}

		// Parse hashed subpackets for signature creation time
		var creationTime int64
		parseSubpackets(hashedData, func(subpacketType byte, data []byte) {
			if subpacketType == pgpSubpacketSignatureCreationTime && len(data) >= 4 {
				creationTime = int64(binary.BigEndian.Uint32(data[:4]))
			}
		})

		// Read unhashed subpacket length and data
		var unhashedLen uint16
		if err := binary.Read(r, binary.BigEndian, &unhashedLen); err != nil {
			return "", fmt.Errorf("failed to read unhashed subpacket length: %w", err)
		}
		unhashedData := make([]byte, unhashedLen)
		if _, err := io.ReadFull(r, unhashedData); err != nil {
			return "", fmt.Errorf("failed to read unhashed subpackets: %w", err)
		}

		// Parse unhashed subpackets for issuer key ID
		parseSubpackets(unhashedData, func(subpacketType byte, data []byte) {
			if subpacketType == pgpSubpacketIssuerKeyID && len(data) >= 8 {
				copy(keyId[:], data[:8])
			}
			// Also check for creation time in unhashed if not found in hashed
			if subpacketType == pgpSubpacketSignatureCreationTime && len(data) >= 4 && creationTime == 0 {
				creationTime = int64(binary.BigEndian.Uint32(data[:4]))
			}
		})

		if creationTime > 0 {
			pkgDate = time.Unix(creationTime, 0).UTC().Format("Mon Jan _2 15:04:05 2006")
		}

	default:
		sig := pgpSig{}
		err := binary.Read(r, binary.BigEndian, &sig)
		if err != nil {
			return "", fmt.Errorf("invalid PGP signature on decode: %w", err)
		}
		pubKeyAlgo = pubKeyLookup[sig.PubKeyAlgo]
		hashAlgo = hashLookup[sig.HashAlgo]
		pkgDate = time.Unix(int64(sig.Date), 0).UTC().Format("Mon Jan _2 15:04:05 2006")
		keyId = sig.KeyID
	}
	return fmt.Sprintf("%s/%s, %s, Key ID %x", pubKeyAlgo, hashAlgo, pkgDate, keyId), nil
}

// parseSubpackets iterates over OpenPGP subpackets and calls the handler for each one.
// Subpacket format per RFC 4880, Section 5.2.3.1:
// Reference: https://www.rfc-editor.org/rfc/rfc4880#section-5.2.3.1
//
// Each subpacket consists of:
// - length (1, 2, or 5 bytes using OpenPGP new-format encoding)
// - type (1 byte, bit 7 is "critical" flag)
// - data (length-1 bytes)
func parseSubpackets(data []byte, handler func(subpacketType byte, data []byte)) {
	for len(data) > 0 {
		// Read subpacket length (variable-length encoding)
		var subpacketLen int
		var headerLen int

		if data[0] < 192 {
			subpacketLen = int(data[0])
			headerLen = 1
		} else if data[0] < 255 {
			if len(data) < 2 {
				return
			}
			subpacketLen = int(data[0]-192)<<8 + int(data[1]) + 192
			headerLen = 2
		} else {
			if len(data) < 5 {
				return
			}
			subpacketLen = int(binary.BigEndian.Uint32(data[1:5]))
			headerLen = 5
		}

		if len(data) < headerLen+subpacketLen || subpacketLen < 1 {
			return
		}

		subpacketType := data[headerLen] & 0x7f // mask off critical bit
		subpacketData := data[headerLen+1 : headerLen+subpacketLen]
		handler(subpacketType, subpacketData)

		data = data[headerLen+subpacketLen:]
	}
}

const (
	sizeOfInt32  = 4
	sizeOfUInt16 = 2
)

func parseInt32Array(data []byte, arraySize int) ([]int32, error) {
	length := arraySize / sizeOfInt32
	values := make([]int32, length)
	reader := bytes.NewReader(data)
	if err := binary.Read(reader, binary.BigEndian, &values); err != nil {
		return nil, xerrors.Errorf("failed to read binary: %w", err)
	}
	return values, nil
}

func parseInt32(data []byte) (int, error) {
	var value int32
	reader := bytes.NewReader(data)
	if err := binary.Read(reader, binary.BigEndian, &value); err != nil {
		return 0, xerrors.Errorf("failed to read binary: %w", err)
	}
	return int(value), nil
}

func uint16Array(data []byte, arraySize int) ([]uint16, error) {
	length := arraySize / sizeOfUInt16
	values := make([]uint16, length)
	reader := bytes.NewReader(data)
	if err := binary.Read(reader, binary.BigEndian, &values); err != nil {
		return nil, xerrors.Errorf("failed to read binary: %w", err)
	}
	return values, nil
}

func parseStringArray(data []byte) []string {
	return strings.Split(string(bytes.TrimRight(data, "\x00")), "\x00")
}

func (p *PackageInfo) InstalledFileNames() ([]string, error) {
	if p == nil || len(p.DirNames) == 0 || len(p.DirIndexes) == 0 || len(p.BaseNames) == 0 {
		return nil, nil
	}

	// ref. https://github.com/rpm-software-management/rpm/blob/rpm-4.14.3-release/lib/tagexts.c#L68-L70
	if len(p.DirIndexes) != len(p.BaseNames) || len(p.DirNames) > len(p.BaseNames) {
		return nil, xerrors.Errorf("invalid rpm %s", p.Name)
	}

	var filePaths []string
	for i, baseName := range p.BaseNames {
		idx := p.DirIndexes[i]
		if len(p.DirNames) <= int(idx) {
			return nil, xerrors.Errorf("invalid rpm %s", p.Name)
		}
		dir := p.DirNames[idx]
		filePaths = append(filePaths, path.Join(dir, baseName)) // should be slash-separated
	}
	return filePaths, nil
}

func (p *PackageInfo) InstalledFiles() ([]FileInfo, error) {
	fileNames, err := p.InstalledFileNames()
	if err != nil {
		return nil, err
	}

	var files []FileInfo
	for i, fileName := range fileNames {
		var digest, username, groupname string
		var mode uint16
		var size, flags int32

		if p.FileDigests != nil && len(p.FileDigests) > i {
			digest = p.FileDigests[i]
		}

		if p.FileModes != nil && len(p.FileModes) > i {
			mode = p.FileModes[i]
		}

		if p.FileSizes != nil && len(p.FileSizes) > i {
			size = p.FileSizes[i]
		}

		if p.UserNames != nil && len(p.UserNames) > i {
			username = p.UserNames[i]
		}

		if p.GroupNames != nil && len(p.GroupNames) > i {
			groupname = p.GroupNames[i]
		}

		if p.FileFlags != nil && len(p.FileFlags) > i {
			flags = p.FileFlags[i]
		}

		record := FileInfo{
			Path:      fileName,
			Mode:      mode,
			Digest:    digest,
			Size:      size,
			Username:  username,
			Groupname: groupname,
			Flags:     FileFlags(flags),
		}
		files = append(files, record)
	}

	return files, nil
}

func (p *PackageInfo) EpochNum() int {
	if p.Epoch == nil {
		return 0
	}
	return *p.Epoch
}
