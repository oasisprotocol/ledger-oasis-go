package internal

import (
	"encoding/hex"
	"fmt"

	"github.com/oasisprotocol/oasis-core/go/common/crypto/signature"
	"github.com/oasisprotocol/oasis-core/go/common/logging"
	ledger_go "github.com/zondax/ledger-go"
)

const (
	CLAConsumer  = 0x05
	CLAValidator = 0xF5

	INSGetVersion     = 0
	INSGetAddrEd25519 = 1
	INSSignEd25519    = 2
)

const (
	PayloadChunkInit = 0
	PayloadChunkAdd  = 1
	PayloadChunkLast = 2
)

type LedgerAppMode int

const (
	ValidatorMode LedgerAppMode = 1 + iota
	ConsumerMode
	UnknownMode
)

const (
	// PathPurposeConsensus is set to 43, matching ledger's Validator app
	PathPurposeConsensus uint32 = 43
)

// LedgerOasis represents a connection to the Ledger app.
type LedgerOasis struct {
	device  ledger_go.LedgerDevice
	version VersionInfo
}

var logger = logging.GetLogger(LogModuleName)

// SetLoggerModule sets logging module.
func (ledger *LedgerOasis) SetLoggerModule(module string) {
	logger = logging.GetLogger(module)
}

// GetModeForRole returns the app mode compatible with role.
func GetModeForRole(role signature.SignerRole) LedgerAppMode {
	switch role {
	case signature.SignerConsensus:
		return ValidatorMode
	default:
		return ConsumerMode
	}
}

// Displays existing Ledger Oasis apps by address.
func ListOasisDevices(path []uint32) {
	ledgerAdmin := ledger_go.NewLedgerAdmin()

	for i := 0; i < ledgerAdmin.CountDevices(); i++ {
		ledgerDevice, err := ledgerAdmin.Connect(i)
		if err != nil {
			continue
		}
		defer ledgerDevice.Close()

		app := LedgerOasis{ledgerDevice, VersionInfo{}}
		defer app.Close()

		appVersion, err := app.GetVersion()
		if err != nil {
			continue
		}

		_, address, err := app.GetAddressPubKeyEd25519(path)
		if err != nil {
			continue
		}

		fmt.Printf("============ Device found\n")
		fmt.Printf("Oasis App Version : %x\n", appVersion)
		fmt.Printf("Oasis App Address : %s\n", address)
	}
}

func GetModeForPath(path []uint32) LedgerAppMode {
	switch path[0] {
	case PathPurposeConsensus:
		return ValidatorMode
	default:
		return ConsumerMode
	}
}

// ConnectLedgerOasisApp connects to Oasis app based on address.
func ConnectLedgerOasisApp(seekingAddress string, path []uint32) (*LedgerOasis, error) {
	ledgerAdmin := ledger_go.NewLedgerAdmin()

	mode := GetModeForPath(path)

	for i := 0; i < ledgerAdmin.CountDevices(); i++ {
		ledgerDevice, err := ledgerAdmin.Connect(i)
		if err != nil {
			continue
		}

		app := LedgerOasis{ledgerDevice, VersionInfo{uint8(mode), 0, 0, 0}}

		_, address, err := app.GetAddressPubKeyEd25519(path)
		if err != nil {
			defer app.Close()
			continue
		}
		if seekingAddress == "" || address == seekingAddress {
			return &app, nil
		}
	}
	return nil, fmt.Errorf("no Oasis app with specified address found")
}

// FindLedgerOasisApp finds the Oasis app running in a Ledger device.
func FindLedgerOasisApp() (*LedgerOasis, error) {
	ledgerAdmin := ledger_go.NewLedgerAdmin()

	for i := 0; i < ledgerAdmin.CountDevices(); i++ {
		ledgerDevice, err := ledgerAdmin.Connect(i)
		if err != nil {
			continue
		}

		app := LedgerOasis{ledgerDevice, VersionInfo{}}

		appVersion, err := app.GetVersion()
		if err != nil {
			app.Close()
			continue
		}

		err = app.CheckVersion(*appVersion)
		if err != nil {
			app.Close()
			continue
		}

		return &app, err
	}

	return nil, fmt.Errorf("no Oasis app found")
}

// Close closes a connection with the Oasis user app.
func (ledger *LedgerOasis) Close() error {
	return ledger.device.Close()
}

// VersionIsSupported returns true if the App version is supported by this library.
func (ledger *LedgerOasis) CheckVersion(ver VersionInfo) error {
	return CheckVersion(ver, VersionInfo{0, 0, 3, 0})
}

// getCLA returns the CLA value for the current app mode.
func (ledger *LedgerOasis) getCLA() byte {
	switch LedgerAppMode(ledger.version.AppMode) {
	case ValidatorMode:
		return CLAValidator
	default:
		return CLAConsumer
	}
}

// GetVersion returns the current version of the Oasis user app.
func (ledger *LedgerOasis) GetVersion() (*VersionInfo, error) {
	message := []byte{ledger.getCLA(), INSGetVersion, 0, 0, 0}
	response, err := ledger.device.Exchange(message)

	logger.Debug("GetVersion requested:")
	logger.Debug("message: " + hex.EncodeToString(message))
	logger.Debug("response: " + hex.EncodeToString(response))

	if err != nil {
		logger.Error("error while getting version: %q", err)
		return nil, err
	}

	if len(response) < 4 {
		return nil, fmt.Errorf("invalid response")
	}

	ledger.version = VersionInfo{
		AppMode: response[0],
		Major:   response[1],
		Minor:   response[2],
		Patch:   response[3],
	}

	return &ledger.version, nil
}

// SignEd25519 signs a transaction using Oasis user app
//
// NOTE: This command requires user confirmation on the device.
func (ledger *LedgerOasis) SignEd25519(bip44Path []uint32, context, transaction []byte) ([]byte, error) {
	return ledger.sign(bip44Path, context, transaction)
}

// GetPublicKeyEd25519 retrieves the public key for the corresponding BIP44
// derivation path.
//
// NOTE: This command DOES NOT require user confirmation on the device.
func (ledger *LedgerOasis) GetPublicKeyEd25519(bip44Path []uint32) ([]byte, error) {
	pubkey, _, err := ledger.retrieveAddressPubKeyEd25519(bip44Path, false)
	return pubkey, err
}

// GetAddressPubKeyEd25519 returns the pubkey and address (Bech32-encoded).
//
// NOTE: This command DOES NOT require user confirmation on the device.
func (ledger *LedgerOasis) GetAddressPubKeyEd25519(bip44Path []uint32) (pubkey []byte, addr string, err error) {
	return ledger.retrieveAddressPubKeyEd25519(bip44Path, false)
}

// ShowAddressPubKeyEd25519 returns the pubkey (compressed) and address (Bech32-encoded).
//
// NOTE: This command requires user confirmation on the device.
func (ledger *LedgerOasis) ShowAddressPubKeyEd25519(bip44Path []uint32) (pubkey []byte, addr string, err error) {
	return ledger.retrieveAddressPubKeyEd25519(bip44Path, true)
}

func (ledger *LedgerOasis) GetBip44bytes(bip44Path []uint32, hardenCount int) ([]byte, error) {
	pathBytes, err := GetBip44bytes(bip44Path, hardenCount)
	if err != nil {
		return nil, err
	}

	return pathBytes, nil
}

func (ledger *LedgerOasis) sign(bip44Path []uint32, context, transaction []byte) ([]byte, error) {
	pathBytes, err := ledger.GetBip44bytes(bip44Path, 5)
	if err != nil {
		return nil, err
	}

	chunks, err := prepareChunks(pathBytes, context, transaction, userMessageChunkSize)
	if err != nil {
		return nil, err
	}

	var finalResponse []byte

	var message []byte

	var chunkIndex int = 0

	for chunkIndex < len(chunks) {
		payloadLen := byte(len(chunks[chunkIndex]))

		var payloadDesc byte
		switch chunkIndex {
		case 0:
			payloadDesc = PayloadChunkInit
		case len(chunks) - 1:
			payloadDesc = PayloadChunkLast
		default:
			payloadDesc = PayloadChunkAdd
		}

		message = []byte{ledger.getCLA(), INSSignEd25519, payloadDesc, 0, payloadLen}
		message = append(message, chunks[chunkIndex]...)

		response, err := ledger.device.Exchange(message)

		logger.Debug("Sign requested:")
		logger.Debug("message: " + hex.EncodeToString(message))
		logger.Debug("response: " + hex.EncodeToString(response))

		if err != nil {
			if err.Error() == "[APDU_CODE_BAD_KEY_HANDLE] The parameters in the data field are incorrect" {
				// In this special case, we can extract additional info
				errorMsg := string(response)
				return nil, fmt.Errorf(errorMsg)
			}
			if err.Error() == "[APDU_CODE_DATA_INVALID] Referenced data reversibly blocked (invalidated)" {
				errorMsg := string(response)
				return nil, fmt.Errorf(errorMsg)
			}
			if err.Error() == "[APDU_CODE_COMMAND_NOT_ALLOWED] Sign request rejected" {
				errorMsg := string(response)
				return nil, fmt.Errorf(errorMsg)
			}
			return nil, err
		}

		finalResponse = response
		chunkIndex++
	}
	return finalResponse, nil
}

// retrieveAddressPubKeyEd25519 returns the pubkey and address (Bech32-encoded).
func (ledger *LedgerOasis) retrieveAddressPubKeyEd25519(
	bip44Path []uint32,
	requireConfirmation bool,
) (pubkey []byte, addr string, err error) {
	pathBytes, err := ledger.GetBip44bytes(bip44Path, 5)
	if err != nil {
		return nil, "", err
	}

	p1 := byte(0)
	if requireConfirmation {
		p1 = byte(1)
	}

	// Prepare message
	header := []byte{ledger.getCLA(), INSGetAddrEd25519, p1, 0, 0}
	message := append(header, pathBytes...)
	message[4] = byte(len(message) - len(header)) // update length

	response, err := ledger.device.Exchange(message)

	logger.Debug("PubKey requested:")
	logger.Debug("message: " + hex.EncodeToString(message))
	logger.Debug("response: " + hex.EncodeToString(response))

	if err != nil {
		return nil, "", err
	}
	if len(response) < 39 {
		return nil, "", fmt.Errorf("invalid response")
	}

	pubkey = response[0:32]
	addr = string(response[32:])

	return pubkey, addr, err
}
