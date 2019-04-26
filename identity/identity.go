package identity

import (
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"os"

	logging "github.com/ipfs/go-log"
	"github.com/ipfs/ipfs-cluster/config"
	"github.com/kelseyhightower/envconfig"
	crypto "github.com/libp2p/go-libp2p-crypto"
	peer "github.com/libp2p/go-libp2p-peer"
	pnet "github.com/libp2p/go-libp2p-pnet"
)

var logger = logging.Logger("identity")

// Identity defaults
const (
	DefaultConfigCrypto    = crypto.RSA
	DefaultConfigKeyLength = 2048
)

// Identity contains information about identity of a peer
type Identity struct {
	// Libp2p ID and private key for Cluster communication (including)
	// the Consensus component.
	ID         peer.ID
	PrivateKey crypto.PrivKey

	// User-defined peername for use as human-readable identifier.
	Peername string

	// Cluster secret for private network. Peers will be in the same cluster if and
	// only if they have the same ClusterSecret. The cluster secret must be exactly
	// 64 characters and contain only hexadecimal characters (`[0-9a-f]`).
	Secret []byte
}

// identityJSON represents a Identity as it will look when it is
// saved using JSON. Most keys are converted into simple types
// like strings, and key names aim to be self-explanatory for the user.
type identityJSON struct {
	ID         string `json:"id"`
	Peername   string `json:"peername"`
	PrivateKey string `json:"private_key"`
	Secret     string `json:"secret"`
}

// Default will generate a valid random ID, PrivateKey and
// Secret.
func (id *Identity) Default() error {
	hostname, err := os.Hostname()
	if err != nil {
		hostname = ""
	}
	id.Peername = hostname

	// pid and private key generation --
	priv, pub, err := crypto.GenerateKeyPair(
		DefaultConfigCrypto,
		DefaultConfigKeyLength)
	if err != nil {
		return err
	}
	pid, err := peer.IDFromPublicKey(pub)
	if err != nil {
		return err
	}
	id.ID = pid
	id.PrivateKey = priv
	// --

	// cluster secret
	clusterSecret, err := pnet.GenerateV1Bytes()
	if err != nil {
		return err
	}
	id.Secret = (*clusterSecret)[:]
	// --
	return nil
}

// LoadJSON receives a raw json-formatted identity and
// sets the Config fields from it. Note that it should be JSON
// as generated by ToJSON().
func (id *Identity) LoadJSON(raw []byte) error {
	jID := &identityJSON{}
	err := json.Unmarshal(raw, jID)
	if err != nil {
		logger.Error("Error unmarshaling cluster config")
		return err
	}

	hostname, err := os.Hostname()
	if err != nil {
		hostname = ""
	}
	id.Peername = hostname

	return id.applyConfigJSON(jID)
}

func (id *Identity) applyConfigJSON(jID *identityJSON) error {
	pid, err := peer.IDB58Decode(jID.ID)
	if err != nil {
		err = fmt.Errorf("error decoding cluster ID: %s", err)
		return err
	}
	id.ID = pid

	config.SetIfNotDefault(jID.Peername, &id.Peername)

	pkb, err := base64.StdEncoding.DecodeString(jID.PrivateKey)
	if err != nil {
		err = fmt.Errorf("error decoding private_key: %s", err)
		return err
	}
	pKey, err := crypto.UnmarshalPrivateKey(pkb)
	if err != nil {
		err = fmt.Errorf("error parsing private_key ID: %s", err)
		return err
	}
	id.PrivateKey = pKey

	clusterSecret, err := DecodeClusterSecret(jID.Secret)
	if err != nil {
		err = fmt.Errorf("error loading cluster secret from config: %s", err)
		return err
	}
	id.Secret = clusterSecret

	return id.Validate()
}

// Validate will check that the values of this identity
// seem to be working ones.
func (id *Identity) Validate() error {
	if id.ID == "" {
		return errors.New("cluster.ID not set")
	}

	if id.PrivateKey == nil {
		return errors.New("no cluster.private_key set")
	}

	return nil
}

// ToJSON generates a human-friendly version of Identity.
func (id *Identity) ToJSON() (raw []byte, err error) {
	jID, err := id.toIdentityJSON()
	if err != nil {
		return
	}

	raw, err = json.MarshalIndent(jID, "", "    ")
	return
}

func (id *Identity) toIdentityJSON() (jID *identityJSON, err error) {
	// Multiaddress String() may panic
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("%s", r)
		}
	}()

	jID = &identityJSON{}

	// Private Key
	pkeyBytes, err := id.PrivateKey.Bytes()
	if err != nil {
		return
	}
	pKey := base64.StdEncoding.EncodeToString(pkeyBytes)

	// Set all identity fields
	jID.ID = id.ID.Pretty()
	jID.Peername = id.Peername
	jID.PrivateKey = pKey
	jID.Secret = EncodeProtectorKey(id.Secret)

	return
}

// DecodeClusterSecret parses a hex-encoded string, checks that it is exactly
// 32 bytes long and returns its value as a byte-slice.x
func DecodeClusterSecret(hexSecret string) ([]byte, error) {
	secret, err := hex.DecodeString(hexSecret)
	if err != nil {
		return nil, err
	}
	switch secretLen := len(secret); secretLen {
	case 0:
		logger.Warning("Cluster secret is empty, cluster will start on unprotected network.")
		return nil, nil
	case 32:
		return secret, nil
	default:
		return nil, fmt.Errorf("input secret is %d bytes, cluster secret should be 32", secretLen)
	}
}

// EncodeProtectorKey converts a byte slice to its hex string representation.
func EncodeProtectorKey(secretBytes []byte) string {
	return hex.EncodeToString(secretBytes)
}

// // ApplyEnvVars overrides configuration fields with any values found
// // in environment variables.
// func (cfg *Manager) ApplyEnvVars() error {
// 	for _, section := range cfg.sections {
// 		for k, compcfg := range section {
// 			logger.Debugf("applying environment variables conf for %s", k)
// 			err := compcfg.ApplyEnvVars()
// 			if err != nil {
// 				return err
// 			}
// 		}
// 	}

// 	if cfg.clusterConfig != nil {
// 		logger.Debugf("applying environment variables conf for cluster")
// 		err := cfg.clusterConfig.ApplyEnvVars()
// 		if err != nil {
// 			return err
// 		}
// 	}

// 	return nil
// }

// // LoadJSONFromFile reads a Configuration file from disk and parses
// // it. See LoadJSON too.
// func (cfg *Manager) LoadJSONFromFile(path string) error {
// 	cfg.path = path

// 	file, err := ioutil.ReadFile(path)
// 	if err != nil {
// 		logger.Error("error reading the configuration file: ", err)
// 		return err
// 	}

// 	err = cfg.LoadJSON(file)
// 	return err
// }

// // LoadJSONFileAndEnv calls LoadJSONFromFile followed by ApplyEnvVars,
// // reading and parsing a Configuration file and then overriding fields
// // with any values found in environment variables.
// func (cfg *Manager) LoadJSONFileAndEnv(path string) error {
// 	if err := cfg.LoadJSONFromFile(path); err != nil {
// 		return err
// 	}

// 	return cfg.ApplyEnvVars()
// }

// Clean removes the file at the given path
func Clean(path string) {
	os.Remove(path)
}

// ApplyEnvVars fills in any Config fields found
// as environment variables.
func (id *Identity) ApplyEnvVars() error {
	jID, err := id.toIdentityJSON()
	if err != nil {
		return err
	}

	err = envconfig.Process("CLUSTER", jID)
	if err != nil {
		return err
	}

	return id.applyConfigJSON(jID)
}

// SaveJSON saves the JSON representation of the Identity to
// the given path.
func (id *Identity) SaveJSON(path string) error {
	logger.Info("Saving configuration")

	bs, err := id.ToJSON()
	if err != nil {
		return err
	}

	return ioutil.WriteFile(path, bs, 0600)
}
