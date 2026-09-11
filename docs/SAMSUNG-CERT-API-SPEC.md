# Samsung Tizen TV Certificate Issuance — Implementation Spec (Go)

**Purpose:** programmatically generate a valid Samsung Tizen TV security profile (author + distributor
certificates) without Tizen Studio's Certificate Manager GUI.

**Authority of this document:** unless marked otherwise, every constant below was extracted by
decompiling Samsung's own current Certificate Extension binary:

```
https://download.tizen.org/sdk/extensions/tizen-certificate-extension_2.0.75.zip
  └─ binary/cert-add-on_2.0.75_macos-64.zip            (…_ubuntu-64.zip / …_windows-64.zip identical)
      └─ data/tools/certificate-manager/Certificate-manager.app/Contents/Eclipse/plugins/
           org.tizen.common.cert_1.0.0.202510211034.jar      ← built 2025-10-21, latest published
```

2.0.75 is the newest version on `download.tizen.org/sdk/extensions/` (2.0.62 → 2.0.75 listed).
Decompiled with `jadx`. Local cross-checks used the installed Tizen Studio at `~/tizen-studio`
(`org.tizen.common.sign_2.0.0.202504091144.jar`, `org.tizen.common_2.0.0.202504091144.jar`).

> ⚠️ **Breaking change, Oct 2025.** The endpoints every public tutorial and OSS tool cites
> (`https://dev.tizen.samsung.com/apis/v2/...`) are **superseded**. The current client uses
> `https://svdca.samsungqbe.com/apis/v3/...`. Build against v3.

---

## 1. Samsung Account OAuth login flow

### 1.1 Constants

Source: `org/tizen/common/cert/ui/SigninDialog.java`

| Constant | Value |
|---|---|
| `SERVICE_ID` (client id) | `v285zxnl3h` |
| `redirectUrl` | `http://localhost:4794/signin/callback` |
| callback port | `4794` (hardcoded `serverPort` in `SignInServer`) |
| callback path | `/signin/callback` |
| `TOKEN_EXPIRY` field | `access_token_expires_in` |

Login URL (verbatim from the decompiled `loginUrl` field — note it is a single literal, no runtime
param assembly):

```
https://account.samsung.com/mobile/account/check.do?serviceID=v285zxnl3h&actionID=StartOAuth2&accessToken=Y&redirect_uri=http://localhost:4794/signin/callback
```

There is **no** `response_type`, `state`, `scope`, or `countryCode` parameter in this flow.
`accessToken=Y` is what makes Samsung return a finished token directly instead of a code.

The extension opens this in the system browser (`SigninDialog.OpenExternalBrowser()`):

```java
String os = System.getProperty("os.name").toLowerCase();
// windows
Runtime.getRuntime().exec("rundll32 url.dll,FileProtocolHandler https://account.samsung.com/mobile/account/check.do?serviceID=v285zxnl3h&actionID=StartOAuth2&accessToken=Y&redirect_uri=http://localhost:4794/signin/callback");
// macos
Runtime.getRuntime().exec("open https://account.samsung.com/...");
// linux
Runtime.getRuntime().exec("xdg-open https://account.samsung.com/...");
```

### 1.2 What comes back

Samsung performs an **HTTP POST** to `http://localhost:4794/signin/callback` with a URL-encoded form
body. Not a GET, not a URL fragment. The handler reads a field literally named **`code`** — which is
*not* an OAuth authorization code; URL-decoding it yields a **JSON object**:

```json
{
  "access_token": "...",
  "userId": "...",
  "inputEmailID": "user@example.com",
  "access_token_expires_in": "..."
}
```

`org/tizen/common/cert/ui/SignInServer.java` (`com.sun.net.httpserver.HttpServer`, no framework):

```java
public void handle(HttpExchange httpExchange) throws Throwable {
    InputStream is = httpExchange.getRequestBody();
    StringBuilder sb = new StringBuilder();
    BufferedReader br = new BufferedReader(new InputStreamReader(is, "UTF-8"));
    String read;
    while ((read = br.readLine()) != null) sb.append(read);
    br.close();
    String decodedStr = URLDecoder.decode(sb.toString(), "UTF-8");
    String[] pairs = decodedStr.split("&");
    Map<String, String> keyValueMap = new HashMap<>();
    for (String pair : pairs) {
        String[] keyVal = pair.split("=");
        keyValueMap.put(keyVal[0], keyVal[1]);
    }
    SignInServer.this.jsonResponse = keyValueMap.get("code");
    SignInServer.this.send_response("Login Successful. Please continue on the Certificate Manager UI", httpExchange);
}
```

**Go implementation:** bind `127.0.0.1:4794`, serve `POST /signin/callback`, `ParseForm()`, take
`r.PostFormValue("code")`, `json.Unmarshal` it. Map the three fields you need:
`access_token` → `access_token` form field, `userId` → `user_id` form field, `inputEmailID` →
distributor CSR `EmailAddress` RDN.

### 1.3 Token exchange

**There is none.** `SigninDialog` contains a vestigial constant
`API_URL = "https://api.samsungosp.com/v2/license/security/authorizeToken?authToken="` and fields
`apiUrl`/`authUrl`, but they are only ever assigned `null` and never read in the current build.
The token from §1.2 is used directly. No `auth.samsungosp.com/auth/oauth2/token` call exists.

### 1.4 Known breakage history

| client id | status |
|---|---|
| `4fb7fnf3np` | original; stopped working ~Nov 2024 |
| `v285zxnl3h` | changed 2024-11-16; **current** in the 2025-10-21 binary |

**Not confirmed:** no tool was found (official or OSS) that bypasses the browser by POSTing
credentials directly to a Samsung signin endpoint. Treat "a human completes the browser login" as
required. Absence of evidence, not proof of impossibility.

Also present: a dead code path in `SigninDialog` using an embedded SWT `Browser` + `LocationListener`
watching for `signInSuccessOAuth2.do` and parsing `access_token=`/`userId=`/`inputEmailID=` as query
params. `SigninDialog.open()` unconditionally calls `OpenExternalBrowser()`, so this path is unused.

---

## 2. Author certificate issuance

### 2.1 Endpoint

```
POST https://svdca.samsungqbe.com/apis/v3/authors
Content-Type: multipart/form-data; boundary="*****"
```

From `org/tizen/common/cert/model/CertConstant.java`:

```java
public static final String AUTHOR_URL          = "https://svdca.samsungqbe.com/apis/v3/authors";
public static final String DISTRIBUTOR_URL     = "https://svdca.samsungqbe.com/apis/v3/distributors";
public static final String DISTRIBUTOR_URL_OLD = "https://svdca.samsungqbe.com/apis/v1/distributors";
```

### 2.2 The POST (base class, shared by author + distributor)

`org/tizen/common/cert/util/CertificateGenerator.java`. Note the **boundary is the literal `*****`**,
the header name is lowercase `content-type`, timeouts are 40 s, and **TLS verification is disabled**
(an all-trusting `X509TrustManager` is installed — you do *not* need to replicate that in Go; normal
verification works against these hosts).

```java
con2.setRequestMethod("POST");
con2.setRequestProperty("content-type", "multipart/form-data; boundary=\"*****\"");
con2.setConnectTimeout(40000);
con2.setReadTimeout(40000);

StringBuffer postDataBuilder = new StringBuffer();
postDataBuilder.append("--*****\r\n");
postDataBuilder.append("Content-Type: text/plain; charset=utf-8\r\n");
postDataBuilder.append("Content-Disposition: form-data; name=access_token\r\n");
postDataBuilder.append("\r\n");
postDataBuilder.append(this.accessInfo.getAccessToken() + "\r\n");
postDataBuilder.append("--*****\r\n");
postDataBuilder.append("Content-Type: text/plain; charset=utf-8\r\n");
postDataBuilder.append("Content-Disposition: form-data; name=user_id\r\n");
postDataBuilder.append("\r\n");
postDataBuilder.append(this.accessInfo.getUserId() + "\r\n");
addContents(postDataBuilder);              // subclass-specific fields, see §3.2
if (this.bVDMode) {                        // TV
    postDataBuilder.append("--*****\r\n");
    postDataBuilder.append("Content-Type: text/plain; charset=utf-8\r\n");
    postDataBuilder.append("Content-Disposition: form-data; name=platform\r\n");
    postDataBuilder.append("\r\n");
    postDataBuilder.append("VD\r\n");
}
postDataBuilder.append("--*****\r\n");
String filename = this.csrName.toString();   // "author.csr" / "distributor.csr"
postDataBuilder.append("Content-Disposition: form-data; name=csr; filename=" + filename
                       + "; filename*=utf-8''" + filename + "\r\n\r\n");
// ... raw CSR bytes streamed here ...
// then: "\r\n" + "--*****--\r\n"
```

**Fields for author (TV):** `access_token`, `user_id`, `platform=VD`, `csr`.
There is **no** `Authorization: Bearer` header — only form fields. (The Python OSS tool adds one; it
is not what Samsung's client does.)

Note the `csr` part carries **no `Content-Type` header** of its own, and uses both `filename=` and
RFC 5987 `filename*=utf-8''`.

### 2.3 Response and errors

- `200` → body is written verbatim to `author.crt`. It is **PEM** (`-----BEGIN CERTIFICATE-----`),
  read as UTF-8 text.
- `403` → rate limited: *"You've made too many requests in a given amount of time."*
- other → body is JSON: `{"error":{"name":..,"status":..,"code":..,"description":..}}`.
  Special case: `status=401` + `code=303` means the Samsung account has no email address on file;
  the GUI sends the user to `https://account.samsung.com/membership/getProfileInfo.do`.

### 2.4 CSR requirements

`org/tizen/common/cert/CSRGenerator/CSRGeneratorForTizen.java` — **RSA 2048**, signature
**`SHA512withRSA`** (not SHA256, not SHA1), PEM type `CERTIFICATE REQUEST`:

```java
KeyPairGenerator keyGen = KeyPairGenerator.getInstance("RSA");
keyGen.initialize(2048, new SecureRandom());
KeyPair keypair = keyGen.generateKeyPair();
...
String params = "CN=" + arg[0] + ", OU=" + arg[1] + ", O=" + arg[2]
              + ", L=" + arg[3] + ", S="  + arg[4] + ", C=" + arg[5];
X500Principal principal = new X500Principal(params);
PKCS10CertificationRequest kpGen = new PKCS10CertificationRequest(
        "SHA512withRSA", principal, publicKey, new DERSet(), privateKey, (String) null);
PemObject pemObject = new PemObject("CERTIFICATE REQUEST", kpGen.getEncoded());
```

`arg[]` comes from `AuthorCreationPage.getInfoList()`:

```java
return new String[]{ getUsername(), getDepartment(), getOrganization(),
                     getCity(), getState(), getCountry() };
```

So the mapping is: `CN`=username, `OU`=department, `O`=organization, `L`=city, `S`=state, `C`=country.
Only `CN` is mandatory in practice (the OSS tools send `-subj "/CN=<email>"` alone and it works);
empty components are tolerated. `C` should be a 2-letter country code.

**Private key side-file.** The key is written to `author.pri` as **DER-encoded PKCS#8
`EncryptedPrivateKeyInfo`** (raw DER, *not* PEM), encrypted with **`PBEWithSHA1AndDESede`**,
random 8-byte salt, **iteration count 20**, password = the profile password:

```java
byte[] salt = new byte[8]; new Random().nextBytes(salt);
PBEParameterSpec pbeParamSpec = new PBEParameterSpec(salt, 20);
PBEKeySpec pbeKeySpec = new PBEKeySpec(password.toCharArray());
SecretKeyFactory keyFac = SecretKeyFactory.getInstance("PBEWithSHA1AndDESede");
SecretKey pbeKey = keyFac.generateSecret(pbeKeySpec);
Cipher pbeCipher = Cipher.getInstance("PBEWithSHA1AndDESede");
pbeCipher.init(1, pbeKey, pbeParamSpec);
byte[] ciphertext = pbeCipher.doFinal(keypair.getPrivate().getEncoded());
AlgorithmParameters algparms = AlgorithmParameters.getInstance("PBEWithSHA1AndDESede");
algparms.init(pbeParamSpec);
byte[] encryptedPkcs8 = new EncryptedPrivateKeyInfo(algparms, ciphertext).getEncoded();
```

In Go you control the whole pipeline, so you can keep the key in memory and skip `.pri` entirely —
it is only an intermediate for Samsung's own Java code. Write it only if you want on-disk parity.

### 2.5 CA chain for author.p12

Bundled **inside the jar** at `res/ca/`, not downloadable standalone. Full inventory:

```
res/ca/gear_test_author_CA.cer          res/ca/vd_tizen_dev_author_ca.cer     ← TV author CA
res/ca/gear_test_partner_CA.cer         res/ca/vd_tizen_dev_partner2.crt
res/ca/gear_test_platform_CA.cer        res/ca/vd_tizen_dev_platform2.crt
res/ca/gear_test_public_CA.cer          res/ca/vd_tizen_dev_public2.crt       ← TV public dist CA
res/ca/samsung_tizen_dev_partner2.crt   res/verifyCa/author_ca.crt
res/ca/samsung_tizen_dev_platform2.crt
res/ca/samsung_tizen_dev_public2.crt
```

For **TV (VD mode)** the author CA is `vd_tizen_dev_author_ca.cer` (`CertConstant.VD_AUTHOR_CA`),
selected by `AuthorGenerator.convertPkcs12()`:

```java
String ca = !this.bVDMode ? CertConstant.AUTHOR_CA.toString()      // gear_test_author_CA.cer
                          : CertConstant.VD_AUTHOR_CA.toString();  // vd_tizen_dev_author_ca.cer
```

**Recommendation:** vendor these `.cer`/`.crt` files into your Go binary with `go:embed`. They are
static Samsung CA certs. Pulling them at runtime means depending on the extension zip layout.

---

## 3. Distributor certificate issuance

### 3.1 Endpoint selection — TV does TWO calls

`DistributorGenerator.generateCSR()` + `fetchCRT()`. For TV, `bVDMode` forces `DeviceVersion.VD`,
which routes to the **v1** endpoint for the `device-profile.xml` intermediate, and then a **second**
call to **v3** for the actual `distributor.crt`:

```java
this.deviceVersion = getMinVersion();
if (this.bVDMode) this.deviceVersion = DeviceVersion.VD;
switch (deviceVersion) {
    case V0: this.url = DISTRIBUTOR_URL_OLD;  this.intermediate = CERTIFICATE_REGISTRATION_XML; break;
    case V1:
    case VD: this.url = DISTRIBUTOR_URL_OLD;  this.intermediate = DEVICE_PROFILE_XML;           break;
    case V2: this.url = DISTRIBUTOR_URL;                                                        break;
    default: throw new Exception("The DUID format is wrong. Enter the value again.");
}
...
protected void fetchCRT() throws Exception {
    fetchCRT(this.url, this.intermediate);                         // v1 → device-profile.xml
    if (this.bVDMode) fetchCRT(CertConstant.DISTRIBUTOR_URL,       // v3 → distributor.crt
                               this.crtName.toString());
}
```

So for a TV profile you issue:
1. `POST https://svdca.samsungqbe.com/apis/v1/distributors` → saved as `device-profile.xml`
2. `POST https://svdca.samsungqbe.com/apis/v3/distributors` → saved as `distributor.crt` (PEM)

and `convertPkcs12()` for the `VD` case uses only `distributor.crt` (case 3/4 fall through to
`generatePkcs12Certificate`). The `device-profile.xml` from step 1 is *not* consumed for VD — but the
call is still made by the official client. **Not verified by live call** whether step 1 can be
skipped; mirror both to be safe.

`DeviceVersion` (`org/tizen/common/cert/model/DeviceVersion.java`):

```java
V0("", 0), V1("1.0", 1), V2("2.0", 2), VD("-", 3);
public String getPrefix() { return prefix.equals("") ? "" : prefix + "#"; }
public static DeviceVersion getVersion(String duid) {
    String prefix = !duid.contains("#") ? "" : duid.split("#")[0];
    for (DeviceVersion dv : values()) if (dv.prefix.equals(prefix)) return dv;
    throw new IllegalArgumentException("Non-compatible Duid");
}
public static String getNoPrefixDuid(String duid) {
    return !duid.contains("#") ? duid : duid.split("#")[1];
}
```

### 3.2 Extra form fields

`DistributorGenerator.addContents()` — appended after `user_id`, before `platform`:

```java
postDataBuilder.append("--*****\r\n");
postDataBuilder.append("Content-Type: text/plain; charset=utf-8\r\n");
postDataBuilder.append("Content-Disposition: form-data; name=privilege_level\r\n");
postDataBuilder.append("\r\n");
postDataBuilder.append(this.privilege + "\r\n");            // Public | Partner | Platform
postDataBuilder.append("--*****\r\n");
postDataBuilder.append("Content-Type: text/plain; charset=utf-8\r\n");
postDataBuilder.append("Content-Disposition: form-data; name=developer_type\r\n");
postDataBuilder.append("\r\n");
postDataBuilder.append("Individual\r\n");
if (this.deviceVersion == DeviceVersion.V0) {               // Gear 2 only, not TV
    ... name=platform ... "Gear2\r\n";
}
```

Full TV distributor field set: `access_token`, `user_id`, `privilege_level`, `developer_type=Individual`,
`platform=VD`, `csr`.

`privilege_level` values come from the enum `PrivilegeLevel { Public, Partner, Platform }`
(`Builder.setPrivilege()` does `PrivilegeLevel.valueOf(privilege)`, so the string is **case-sensitive,
capitalised**). For test-lab TVs use `Public`.

`developer_type` is hardcoded `Individual` in the client — no other value observed.

### 3.3 CSR + SubjectAltName (the DUID embedding)

Subject is fixed: `infoList = {"TizenSDK","","","","",""}` → `CN=TizenSDK` plus the account email.
`CSRGeneratorForTizen.generateCSRDistribute()`:

```java
String params = "CN=" + arg[0] + ", OU=" + arg[1] + ", O=" + arg[2] + ", L=" + arg[3]
              + ", ST=" + arg[4] + ", C=" + arg[5] + ", EmailAddress=\"" + email + "\"";
X509Principal principal = new X509Principal(params);

Vector<ASN1ObjectIdentifier> oids = new Vector<>();
ASN1EncodableVector uriVector = new ASN1EncodableVector();
oids.add(X509Extensions.SubjectAlternativeName);
uriVector.add(new GeneralName(6, "URN:tizen:packageid="));
for (String deviceId : deivceIds) {
    uriVector.add(new GeneralName(6, "URN:tizen:deviceid=" + deviceId));
}
GeneralNames subjectAltNames = GeneralNames.getInstance(new DERSequence(uriVector));
DERSequence altNameExtension = new DERSequence(new ASN1Encodable[]{
        X509Extensions.SubjectAlternativeName, new DEROctetString(subjectAltNames)});
DERSet extensions = new DERSet(new DERSequence(altNameExtension));
DERSequence extensionRequest = new DERSequence(new ASN1Encodable[]{
        PKCSObjectIdentifiers.pkcs_9_at_extensionRequest, extensions});
DERSet attribute = new DERSet(extensionRequest);
PKCS10CertificationRequest kpGen = new PKCS10CertificationRequest(
        "SHA512withRSA", principal, publicKey, attribute, privateKey, (String) null);
```

**Exact SAN format — settles the question:**

- GeneralName tag **`6` = uniformResourceIdentifier (URI)**.
- First entry is always, literally: `URN:tizen:packageid=` (empty value, always present).
- Each DUID: `URN:tizen:deviceid=<DUID>`.

So it is **`URI:URN:tizen:deviceid=<DUID>`** — an URI-type SAN whose *string value* is URN-shaped.
It is **not** `URI:http://tizen.org/deviceid/<DUID>` and **not** `URN:tizen:deviceid:<DUID>`.
Note the `=` separator, not `:`. The openssl equivalent:

```
-addext "subjectAltName = URI:URN:tizen:packageid=,URI:URN:tizen:deviceid=<DUID>[,URI:URN:tizen:deviceid=<DUID2>...]"
```

Also note the distributor subject uses `ST=` (via BouncyCastle `X509Principal`) whereas the author
subject uses `S=` (via JDK `X500Principal`). Both encode OID 2.5.4.8 (stateOrProvinceName).

**In Go:** `x509.CertificateRequest.URIs` takes `*url.URL`. `url.Parse("URN:tizen:deviceid=XXXX")`
yields `Scheme="urn"` and `Opaque="tizen:deviceid=XXXX"`, which re-serialises **lowercased** as
`urn:tizen:deviceid=XXXX`. That case change is a real risk. Safer: build the SAN extension yourself
with `encoding/asn1` — emit `GeneralName` context tag 6 (`asn1.Tag{Class:2, Tag:6}`) with the exact
byte string, and set it as an `ExtraExtensions` entry under OID `2.5.29.17`, then wrap it in the
`pkcs-9-at-extensionRequest` attribute (`1.2.840.113549.1.9.14`).

### 3.4 Max DUID count

**50.** From `org/tizen/common/cert/ui/messages.properties`:

```
DistributorCreationPage_Error_TooManyDuid=All DUIDs do not have the correct format, or there are more than 50 DUIDs in the file. Fix the file and import it again.
```

### 3.5 CA chain for distributor.p12

Chosen by `PrivilegeLevel.getCAPathInPlugin(privilege, deviceVersion)`:

| privilege | non-VD (`V1`/`V2`) | legacy (`V0`) | **TV (`VD`)** |
|---|---|---|---|
| Public | `samsung_tizen_dev_public2.crt` | `gear_test_public_CA.cer` | **`vd_tizen_dev_public2.crt`** |
| Partner | `samsung_tizen_dev_partner2.crt` | `gear_test_partner_CA.cer` | `vd_tizen_dev_partner2.crt` |
| Platform | `samsung_tizen_dev_platform2.crt` | `gear_test_platform_CA.cer` | `vd_tizen_dev_platform2.crt` |

For a Public TV profile: **`vd_tizen_dev_public2.crt`**.

---

## 4. DUID retrieval via sdb

```
sdb connect <TV_IP>              # port 26101 implied; explicit: sdb -s <TV_IP>:26101 shell 0 getduid
sdb shell 0 getduid
```

Output is a single bare alphanumeric token, no separators — commonly reported as ~32 uppercase
alphanumeric chars. Two independent production tools validate it with `^[A-Za-z0-9]{10,64}$`.

**Reliability:** `0 getduid` is flaky on older/slower TVs (empty or truncated first response).
Multiple independent tools wrap it in a 2–3 attempt retry with disconnect/reconnect. Do the same, and
validate against the regex rather than trusting the first read — a failed read answers in prose, not
a token.

**Version prefixes.** The `1.0#` / `2.0#` prefix convention is documented by Samsung only for
**wearables/mobile** (Gear-class), which are read with a *different* command, `sdb shell 0 getduidgadget`.
The `DeviceVersion` enum above is what parses them.

For **TVs this is moot**: `DistributorGenerator.generateCSR()` does
`if (this.bVDMode) this.deviceVersion = DeviceVersion.VD;` — it **overrides** version detection
entirely, so no prefix parsing happens and the DUID string is passed to the CSR **verbatim,
unmodified**. Also `DistributorGenerator.validate()` returns `true` unconditionally in VD mode
(non-VD requires the DUID be valid base64). So: **feed the raw `getduid` output straight into the SAN.**

*Not confirmed:* no byte-for-byte terminal capture of TV `getduid` output was located, so the
"TV DUIDs never carry a `2.0#` prefix" claim is inference from the VD-mode override plus ~10 working
tools passing output through unmodified — strong, but not a direct observation.

**Other sources:** TV menu `Support > Contact Samsung > Unique Device ID` (submenu name drifts across
firmware); Tizen Device Manager auto-populates it. The REST API on `http://<tv>:8001/api/v2/` returns
a `duid` field in **UUID form** — this is a *different* identifier space and is **not** usable for
certificates; no confirmed conversion exists (open, unanswered on Samsung's forum).

---

## 5. Security profile registration (`profiles.xml`)

### 5.1 Schema

Constants from `org/tizen/common/sign/model/SignatureConstants.java`:

```java
PROFILES_FILE = "profiles.xml";   ROOT_ELEMENT = "profiles";
ROOT_ATTR_ACTIVE = "active";      ROOT_ATTR_VER = "version";
PROFILE_ELEMENT = "profile";      PROFILE_ATTR_NAME = "name";
PROFILEITEM_ELEMENT = "profileitem";
PROFILEITEM_ATTR_KEY = "key";     PROFILEITEM_ATTR_CA = "ca";
PROFILEITEM_ATTR_ROOTCA = "rootca";  PROFILEITEM_ATTR_PASS = "password";
PROFILEITEM_ATTR_AUTHOR = "author";  PROFILEITEM_ATTR_DISTRIBUTOR = "distributor";
PROFILES_VERSION = "3.0";         PROFILES_VERSION_NEW = "3.1";
```

Location: `<tizen-studio-data>/profile/profiles.xml`. Real file from this machine:

```xml
<?xml version="1.0" encoding="UTF-8" standalone="no"?>
<profiles active="globoplay-testing" version="3.1">
<profile name="globoplay-testing">
<profileitem ca="" distributor="0" key="/Users/.../SamsungCertificate/globoplay-testing/author.p12"      password="..." rootca=""/>
<profileitem ca="" distributor="1" key="/Users/.../SamsungCertificate/globoplay-testing/distributor.p12" password="..." rootca=""/>
<profileitem ca="" distributor="2" key="" password="" rootca=""/>
</profile>
</profiles>
```

**Attribute semantics:**
- `distributor="0"` = author slot, `"1"` = primary distributor, `"2"` = optional second distributor.
  Ordinal range is `0..2` (`MAX_DISTRIBUTOR = 2`); slot 2 is written **empty but present** — the
  Samsung extension explicitly calls `createEmptyProfileItem(2)`.
- **`version="3.0"` vs `"3.1"` — resolved.** `SigningProfileContainer.readProfileXML()` does
  `if (version.equals(PROFILES_VERSION_NEW)) this.activeProfileName = rootElement.getAttribute(ROOT_ATTR_ACTIVE);`
  i.e. the `active` attribute is **only honoured when `version="3.1"`**. Write `3.1`.
  Legacy `3.0`/versionless files instead used a boolean `author="true"` attribute to identify the
  author slot, and the active profile came from a separate preference key `defaultprofile`.
- `ca` / `rootca`: the Samsung extension writes both as **empty strings** for Samsung-issued
  certs — `createProfileItem(this.ordinal, certificate.getAbsolutePath(), this.password, "", "")`.
  The CA is already inside the p12 (§6), so these are redundant here. `rootca` was empty in every
  real-world sample examined; its semantics remain **unconfirmed**.
- `key`: absolute path to the `.p12`. Samsung writes absolute paths.

The generated cert store path is `CertConstant.STORE_PATH = ~/SamsungCertificate/<profileName>/`.

### 5.2 Password field — three accepted forms

`SigningProfile.readProfileItem()` dispatches on whether the attribute ends in `.pwd`:

```java
private static final String PWD_EXT = ".pwd";
public boolean stored(String passwordAttr) { return !passwordAttr.endsWith(PWD_EXT); }
...
if (!passwordAttr.isEmpty() && !key.isEmpty()) {
    if (stored(passwordAttr)) {
        pass = CipherUtil.getDecryptedString(passwordAttr);   // inline, legacy 3DES
    } else {
        ICrypt dec = CryptFactory.getCrypt();                 // OS credential store
        pass = dec.decrypt(passwordAttr);
    }
}
```

**(a) Inline encrypted — `CipherUtil`, the "KYANITE" question, settled.**
Source: `org/tizen/common/util/CipherUtil.class` in `org.tizen.common_2.0.0.202504091144.jar`
(Tizen Studio 2025 build):

```java
private static final String password = "KYANINYLhijklmnopqrstuvwx";
private static final String ALGORITHM = "DESede";
static {
    byte[] key = password.getBytes();
    DESedeKeySpec desKeySpec = new DESedeKeySpec(key);
    SecretKeyFactory keyFactory = SecretKeyFactory.getInstance(ALGORITHM);
    SECRETE_KEY = keyFactory.generateSecret(desKeySpec);
    DES_CIPHER = Cipher.getInstance("DESede/ECB/PKCS5Padding");
}
public static String getEncryptedString(String s) { return Base64.encode(encryptByDES(s)); }
public static String getDecryptedString(String s) { return new String(decryptByDES(Base64.decode(s))); }
```

| parameter | value |
|---|---|
| cipher | **3DES (`DESede`)** — *not* single DES |
| mode / padding | **ECB / PKCS5Padding** |
| IV | none (ECB) |
| key string | **`KYANINYLhijklmnopqrstuvwx`** (25 chars) — the folklore "KYANITE" is **wrong** |
| key bytes | raw ASCII, `DESedeKeySpec` consumes the **first 24 bytes**: `KYANINYLhijklmnopqrstuvw` (final `x` unused) |
| key hex | `4b59414e494e594c68696a6b6c6d6e6f707172737475 7677` |
| encoding | standard Base64 |

Verified by round-trip on this machine with the JDK shipped in Tizen Studio, and independently
corroborated by decrypting known ciphertexts from public repos (e.g.
`Vy63flx5JBMc5GA4iEf8oFy+8aKE7FX/+arrDcO4I5k=` → `tizenpkcs12passfordsigner`, the well-known default
Tizen distributor signer password; `xmEcrXPl1ss=` → empty string).

Go: `crypto/des.NewTripleDESCipher(key[:24])` + manual ECB loop + PKCS#5 pad + `base64.StdEncoding`.

**(b) `.pwd` path — OS-native credential store. NOT portable, do not use.**
When the attribute ends in `.pwd`, the value is a *path*, and the password lives in the **operating
system credential store**, keyed by that path. `createPasswordAttrVal()` derives it by swapping the
key's `.p12` suffix for `.pwd`, then calls `enc.encrypt(pass, passwordAttr)`:

```java
// org/tizen/common/sign/util/cryptutil/MacCrypt.java
String[] CMD_ARRAY = {"security", "add-generic-password", "-a", filename,
                      "-s", "certificate-manager", "-w", pswd, "-U"};
// LinuxCrypt.java  → <tools>/certificate-encryptor/secret-tool store --label="tizen-studio" \
//                       -p <pwd> keyfile <filename> tool certificate-manager
// WinCrypt.java    → <tools>/certificate-encryptor/wincrypt --encrypt <pwd> <filename>
```

Confirmed empirically on this machine: the keychain entry exists
(`security find-generic-password -a <...>/author.pwd -s certificate-manager` → service
`certificate-manager`), and the on-disk `author.pwd` / `distributor.pwd` are **234-byte binary
blobs**, not plaintext — their header `d08c9ddf 0115 d111 8c7a 00c04fc297eb` is the **Windows DPAPI**
provider GUID `df9d8cd0-1501-11d1-8c7a-00c04fc297eb`. So `.pwd` content is platform-specific
(DPAPI blob on Windows; on macOS/Linux the file is incidental and the secret is in Keychain/libsecret).

> Some community docs claim `.pwd` files contain plaintext. That may have been true in older Tizen
> Studio 3.x, but it is **not** true of the current implementation. Independent reports also document
> `.pwd` support being outright broken in 2025 Tizen tooling (headless installs never create the
> directory, so the write silently fails and signing dies with `CertificationException: Invaild password`).
> **For a Go tool: never emit a `.pwd` path.**

**(c) Plaintext.** `getDecryptedString()` returns the input unchanged when Base64 decoding yields
`null`, and a large number of currently-maintained CI scripts write raw plaintext into `password=""`
and successfully run `tizen package -t wgt -s <profile>`. This works, but the exact fallback path is
behavioural evidence rather than proven source logic — Base64-decodable plaintext could in principle
be misinterpreted.

**Recommendation for the Go implementation:** write **form (a)**, the inline `CipherUtil` 3DES
Base64 value. It is what the tooling canonically expects, it is fully specified above, it is
portable across OSes, and it sidesteps both the `.pwd` breakage and the plaintext ambiguity.

### 5.3 What the extension writes after issuance

`CertificateGenerator.registerProfile()` — note `ca` and `rootca` are deliberately `""`:

```java
File certificate = getFile(null, this.p12Name.toString());
SigningProfileContainer container = SigningProfileUtil.getProfileContainerFromFile();
SigningProfile defaultProfile = container.getProfileByName(this.profile);
if (defaultProfile == null) {
    SigningProfile p = new SigningProfile(this.profile);
    p.createProfileItem(this.ordinal, certificate.getAbsolutePath(), this.password, "", "");
    p.createEmptyProfileItem(2);
    container.addProfile(p);
} else {
    SigningProfileItem item = defaultProfile.getProfileItem(this.ordinal);
    if (item == null) defaultProfile.createProfileItem(this.ordinal, certificate.getAbsolutePath(), this.password, "", "");
    else { item.setKeyLocation(certificate.getAbsolutePath()); item.setPassword(this.password.toCharArray());
           item.setCAPath(""); item.setRootCAPath(""); defaultProfile.setProfileItem(this.ordinal, item); }
    defaultProfile.createEmptyProfileItem(2);
}
SigningProfileUtil.writeProfilesToFile(container);
```

Author uses `ordinal = 0`, distributor `ordinal = 1`.

### 5.4 CLI alternative

If you'd rather shell out than write the XML (both work):

```bash
# classic CLI (still shipped, used by most CI in 2024-2025)
tizen security-profiles add -n <name> -a <author.p12> -p <pw> -d <distributor.p12> -dp <dist_pw>

# tizen-core / `tz` (Tizen 9.0 generation)
tz security-profiles add -n <name> -x <profiles.xml path> -a <author.p12> -p <pw> -d <dist.p12> -P <dist pw> [-A active]
```

*Not confirmed:* whether `tz` uses the same `KYANINYL…` 3DES scheme internally.

Password policy enforced by the GUI (`messages.properties`), worth matching:
*"Minimum 8 characters long; At least 1 lower case letter; At least 1 upper case letter; At least 1 numeral."*

---

## 6. PKCS#12 packaging

### 6.1 What Samsung's own code produces

`ProfileCrypto.GeneratePkcs12Certificate()` — the chain is exactly **[leaf, CA]** (2 entries, no
root), the keystore type is `PKCS12`, and the alias is the literal **`UserCertificate`**:

```java
Certificate trustCert = loadCertificate(bis);                  // the res/ca/*.cer
Certificate[] certificateArray = { loadCertificate(bis2), trustCert };   // [leaf, CA]
KeyStore keyStore = KeyStore.getInstance("PKCS12");
keyStore.load(null, null);
keyStore.setKeyEntry("UserCertificate", privateKey, passwordp12.toCharArray(), certificateArray);
FileOutputStream fOut = new FileOutputStream(filepath);
keyStore.store(fOut, passwordp12.toCharArray());
```

- **Alias / friendlyName:** Samsung's literal is `UserCertificate` (capitalised). Community OSS tools
  use lowercase `usercertificate` and work fine. Neither `author`, `USERKEY`, nor `signer` appears.
- **Same password** is used for the p12 and for the encrypted private key (`generatePkcs12Certificate`
  passes `this.password` for both `privPassword` and `p12Password`).
- The leaf PEM is stripped of its armor and Base64-decoded before being parsed — so the `.crt` from
  the API is definitely PEM.

### 6.2 OpenSSL 3.x compatibility — the legacy cipher problem is real

Tizen's signer runs on an old Java/BouncyCastle that expects a **SHA-1 MAC + PBE-SHA1-3DES** PKCS#12.
OpenSSL 3.x defaults to AES-256-CBC + PBKDF2 + SHA-256 MAC, which is rejected — and misleadingly
surfaces as `CertificationException: Invaild password` (sic). `-legacy` **alone is not sufficient**:
it falls back to 40-bit RC2, which JDK 17 restricts. The full incantation:

```bash
openssl pkcs12 -export -inkey key.pem -in cert-and-ca.pem -out author.p12 \
  -name usercertificate \
  -keypbe PBE-SHA1-3DES -certpbe PBE-SHA1-3DES -macalg sha1 \
  -passout pass:<password> -legacy
```

`-macalg sha1` is the critical, most-often-missed knob. The same applies in reverse — reading a
Samsung-issued p12 with OpenSSL 3.x also needs `-legacy`, because Samsung's own tooling emits
RC2/3DES bags. Note some OpenSSL 3.x builds ship without the legacy provider at all.

**In Go this is mostly a non-issue** — but choose your library deliberately:
- `software.sslmate.com/src/go-pkcs12`: `pkcs12.Modern.Encode(...)` produces AES/PBKDF2 → **will be
  rejected** by Tizen. Use `pkcs12.Legacy.Encode(...)` (or the legacy `pkcs12.Encode`), which emits
  `pbeWithSHAAnd3-KeyTripleDES-CBC` + SHA-1 MAC — matching what Java's `KeyStore` PKCS12 writes.
- Include both certs in the chain: leaf first, then the Samsung CA.

*Not confirmed:* whether a deeper chain (up to a root CA) is ever required — no example needed it,
and none contradicted it.

---

## 7. Sources

### Primary — Samsung's own binary (authoritative; everything in §1–§3, §5, §6 traced here)

`https://download.tizen.org/sdk/extensions/tizen-certificate-extension_2.0.75.zip`
→ `binary/cert-add-on_2.0.75_macos-64.zip`
→ `…/plugins/org.tizen.common.cert_1.0.0.202510211034.jar` (built 2025-10-21), files read:

- `org/tizen/common/cert/model/CertConstant.class` — all endpoint URLs, CA filenames, store path
- `org/tizen/common/cert/model/DeviceVersion.class` — DUID prefix parsing
- `org/tizen/common/cert/model/PrivilegeLevel.class` — privilege → CA mapping
- `org/tizen/common/cert/model/ProfileCrypto.class` — p12 generation, alias, chain
- `org/tizen/common/cert/CSRGenerator/CSRGeneratorForTizen.class` — CSR, SAN, key encryption
- `org/tizen/common/cert/util/CertificateGenerator.class` — multipart POST, errors, registerProfile
- `org/tizen/common/cert/util/AuthorGenerator.class`, `util/DistributorGenerator.class`
- `org/tizen/common/cert/ui/SigninDialog.class`, `ui/SignInServer.class` — OAuth
- `org/tizen/common/cert/ui/messages.properties` — 50-DUID limit, password policy
- `res/ca/*` — the CA certificates themselves

### Primary — local Tizen Studio install (`~/tizen-studio`)

- `tools/ide/lib-ncli/org.tizen.common_2.0.0.202504091144.jar` → `org/tizen/common/util/CipherUtil.class`
  — **the profiles.xml password cipher** (§5.2a)
- `tools/ide/lib-ncli/org.tizen.common.sign_2.0.0.202504091144.jar` →
  `preferences/SigningProfile.class`, `preferences/SigningProfileContainer.class`,
  `model/SignatureConstants.class`, `util/cryptutil/{CryptFactory,MacCrypt,LinuxCrypt,WinCrypt}.class`
- `~/tizen-studio-data/profile/profiles.xml`, `~/SamsungCertificate/globoplay-testing/*` — live samples

### Secondary — open-source reimplementations

- **sreyemnayr/tizencertificates** (Python, last commit 2025-10-20 — most current OSS impl)
  - `tizencertificates/cert_server.py` — OAuth callback, CSR/SAN, API POSTs
  - `tizencertificates/certtool.py` — runtime extraction of constants + CA certs from Samsung's jar
  - Commit `2fd193cf`: *"Update to accomodate new distributer/author endpoints"*
- **gist andreas-mausch/026413d908ba61cda15981bb4fbcd276** — the original curl/openssl writeup (+ its
  8 comments, which date the Oct 2025 endpoint change). Upstream `gitlab.com/andreas-mausch/moonwatch`
  is behind Cloudflare and was unreachable.
- **rgngl/7868448** (gist) — `CipherUtil` Java source, independent confirmation of the 3DES key
- **Apps2Samsung/Apps2Samsung** — `Apps2Samsung.Core/Certificate/CipherUtil.cs` (C# port of the cipher),
  `Certificate/CertificateProvisioningService.cs` (p12 chain/alias/3DES bags),
  `Models/TizenDuid.cs`, `Sdb/TizenDuidReader.cs` (DUID regex + retry)
- **nkatchik/smarttv-twitch** — `tools/release.js` (JS port of the cipher)
- **RumbleCrumbleRS/JellyPlug-Tizen** — `tooling/ci/configure-tizen-ci-cert.sh` (the `-keypbe/-certpbe/-macalg sha1`
  recipe and the `.pwd` failure analysis), `tooling/ci/configure-tizen-signing.sh`
- **christopherklint97/streamvault** — `scripts/deploy-tv.sh`: *"Tizen Studio 2025 broke .pwd file support"*
- **Samsung/vs-tools-cps** — `src/Tizen.VisualStudio.Tools/Data/CertificateProfile.cs` (official
  profiles.xml sample with real encrypted passwords)
- **Samsung/tizen-docs** — `docs/application/dotnet/guides/concepts/signing-certificates/creating-certificates.md`
  (Public vs Partner semantics, 50-DUID limit),
  `docs/sdk-tools/baseline-sdk/tizen-core/tizen-core-cli.md` (`tz security-profiles`)
- DUID via sdb, identical command across: tailscale/tailscale-tizen, jeancarlos/aerial-tizen,
  fliks-app/fliks, SushyDev/tizen-homebrew, reisxd/tizen.js, PatrickSt1991/tizen-sdb,
  SmookeyDev/MoonlightTizenInstaller, ardazeytin/samsung-tizen-tv-debloat

---

## Appendix — end-to-end TV flow

1. Start `127.0.0.1:4794`, open the login URL (§1.1), wait for `POST /signin/callback`, parse `code`
   → `access_token`, `userId`, `inputEmailID`.
2. RSA-2048 keypair; author CSR `CN=<name>, OU=…, O=…, L=…, S=…, C=<CC>`, SHA512withRSA.
3. `POST https://svdca.samsungqbe.com/apis/v3/authors` (multipart, boundary `*****`):
   `access_token`, `user_id`, `platform=VD`, `csr` → PEM `author.crt`.
4. `author.p12` = [author.crt, `vd_tizen_dev_author_ca.cer`] + key, alias `UserCertificate`,
   legacy PBE-SHA1-3DES + SHA-1 MAC.
5. `sdb connect <TV_IP>` → `sdb shell 0 getduid` (retry, validate `^[A-Za-z0-9]{10,64}$`).
6. RSA-2048 keypair; distributor CSR `CN=TizenSDK, …, EmailAddress="<email>"`, SHA512withRSA,
   SAN = `URI:URN:tizen:packageid=` + one `URI:URN:tizen:deviceid=<DUID>` per TV (≤50).
7. `POST …/apis/v1/distributors` → `device-profile.xml`, then
   `POST …/apis/v3/distributors` → PEM `distributor.crt`.
   Both with `access_token`, `user_id`, `privilege_level=Public`, `developer_type=Individual`,
   `platform=VD`, `csr`.
8. `distributor.p12` = [distributor.crt, `vd_tizen_dev_public2.crt`] + key, same packaging rules.
9. Write `profiles.xml` v3.1 with `active="<name>"`, slots `distributor="0"`/`"1"` populated,
   `"2"` empty, `ca=""`/`rootca=""`, passwords as inline `CipherUtil` 3DES/ECB/PKCS5 Base64.
10. `tizen package -t wgt -s <name>` → `tizen install`.
