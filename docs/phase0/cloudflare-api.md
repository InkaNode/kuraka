# Cloudflare API v4: mail DNS and domain redirects

## 1. Authentication and permissions

Base URL: `https://api.cloudflare.com/client/v4`. Send `Authorization: Bearer <TOKEN>` and `Content-Type: application/json` for JSON requests. Verify a user token with `GET /user/tokens/verify` (no body); check `success` and `result.status == "active"`. Verification checks token status, not access to every required operation. [Token verification](https://developers.cloudflare.com/api/resources/user/subresources/tokens/methods/verify/).

Grant `Zone:Zone:Read` plus `Zone:DNS:Edit` (API permission names `Zone Zone Read`, `DNS Write`), restricting Zone Resources to the specific zones being managed. DNS Edit includes DNS reads. For redirects, additionally grant `Zone > Single Redirect > Edit` (`Dynamic URL Redirects Write`). [Zone permission](https://developers.cloudflare.com/api/resources/zones/methods/list/), [DNS permission](https://developers.cloudflare.com/api/resources/dns/subresources/records/methods/create/), [redirect permission](https://developers.cloudflare.com/rules/url-forwarding/single-redirects/create-api/).

## 2. Zone and DNS operations

Paths are relative to the base URL; below, `R` means `/zones/{zone_id}/dns_records`. Replace example domains, addresses, keys, and hashes. `ttl:1` means automatic. POST examples are individual records, not a batch.

| Method | Path | Purpose | Minimal JSON body | Docs |
|---|---|---|---|---|
| GET | `/zones?name=example.com` | Find zone ID by name | None | [List zones](https://developers.cloudflare.com/api/resources/zones/methods/list/) |
| GET | `R?page=1&per_page=100` | List DNS records | None | [List DNS](https://developers.cloudflare.com/api/resources/dns/subresources/records/methods/list/) |
| POST | `R` | A: mail server address | `{"type":"A","name":"mail.example.com","content":"198.51.100.10","ttl":1,"proxied":false}` | [Create](https://developers.cloudflare.com/api/resources/dns/subresources/records/methods/create/) |
| POST | `R` | CNAME: mail alias | `{"type":"CNAME","name":"imap.example.com","content":"mail.example.com","ttl":1,"proxied":false}` | [Create](https://developers.cloudflare.com/api/resources/dns/subresources/records/methods/create/) |
| POST | `R` | MX: lower priority preferred | `{"type":"MX","name":"example.com","content":"mail.example.com","priority":10,"ttl":1}` | [Create](https://developers.cloudflare.com/api/resources/dns/subresources/records/methods/create/) |
| POST | `R` | TXT: SPF | `{"type":"TXT","name":"example.com","content":"\"v=spf1 mx -all\"","ttl":1}` | [Create](https://developers.cloudflare.com/api/resources/dns/subresources/records/methods/create/) |
| POST | `R` | TXT: DKIM; replace key | `{"type":"TXT","name":"s1._domainkey.example.com","content":"\"v=DKIM1; k=rsa; p=<PUBLIC_KEY>\"","ttl":1}` | [TXT schema](https://developers.cloudflare.com/api/resources/dns/) |
| POST | `R` | TXT: DMARC | `{"type":"TXT","name":"_dmarc.example.com","content":"\"v=DMARC1; p=none\"","ttl":1}` | [Create](https://developers.cloudflare.com/api/resources/dns/subresources/records/methods/create/) |
| POST | `R` | SRV: submissions | `{"type":"SRV","name":"_submissions._tcp.example.com","ttl":1,"data":{"priority":0,"weight":1,"port":465,"target":"mail.example.com"}}` | [SRV schema](https://developers.cloudflare.com/api/resources/dns/) |
| POST | `R` | SRV: IMAPS | `{"type":"SRV","name":"_imaps._tcp.example.com","ttl":1,"data":{"priority":0,"weight":1,"port":993,"target":"mail.example.com"}}` | [SRV schema](https://developers.cloudflare.com/api/resources/dns/) |
| POST | `R` | SRV: autodiscover | `{"type":"SRV","name":"_autodiscover._tcp.example.com","ttl":1,"data":{"priority":0,"weight":1,"port":443,"target":"autodiscover.example.com"}}` | [SRV schema](https://developers.cloudflare.com/api/resources/dns/) |
| POST | `R` | Optional CAA | `{"type":"CAA","name":"example.com","ttl":1,"data":{"flags":0,"tag":"issue","value":"letsencrypt.org"}}` | [Create](https://developers.cloudflare.com/api/resources/dns/subresources/records/methods/create/) |
| POST | `R` | Optional TLSA; replace hash | `{"type":"TLSA","name":"_25._tcp.mail.example.com","ttl":1,"data":{"usage":3,"selector":1,"matching_type":1,"certificate":"<SHA256_SPKI_HEX>"}}` | [Create](https://developers.cloudflare.com/api/resources/dns/subresources/records/methods/create/) |
| PATCH | `R/{dns_record_id}` | Update any listed type | Changed fields only: e.g. `{"content":"198.51.100.11"}` for A; `{"priority":20}` for MX; `{"data":{"priority":0,"weight":1,"port":993,"target":"mail2.example.com"}}` for SRV | [Patch](https://developers.cloudflare.com/api/resources/dns/subresources/records/methods/edit/) |
| DELETE | `R/{dns_record_id}` | Delete any listed type | None | [Delete](https://developers.cloudflare.com/api/resources/dns/subresources/records/methods/delete/) |

Pagination: inspect `result_info.page`, `per_page`, `count`, `total_count`, and `total_pages`; increment `page` through `total_pages`. [Pagination](https://developers.cloudflare.com/api/resources/dns/subresources/records/methods/list/).

Long DKIM: keep the entire value in one TXT record. DNS character strings have a 255-byte limit; Cloudflare automatically splits longer strings. JSON-escape the surrounding quotes as above; applications concatenate the resulting chunks. Do not create separate TXT records for chunks. [TXT schema](https://developers.cloudflare.com/api/resources/dns/), [TXT splitting](https://developers.cloudflare.com/dns/faq/).

## 3. Proxy status

Only A, AAAA, and CNAME records support the standard Cloudflare proxy. Set `proxied:false` on mail host addresses and aliases, including MX/SRV targets used for SMTP, submission, or IMAP. MX, TXT, SRV, CAA, and TLSA remain DNS-only; omit `proxied` for them. [Proxy status](https://developers.cloudflare.com/dns/proxy-status/).

## 4. Permanent secondary-to-primary redirect

In the secondary zone, send `PUT /zones/{zone_id}/rulesets/phases/http_request_dynamic_redirect/entrypoint` with this complete minimal body:

```json
{
  "rules": [{
    "expression": "(http.host eq \"secondary.example\")",
    "action": "redirect",
    "action_parameters": {
      "from_value": {
        "status_code": 301,
        "target_url": {
          "expression": "concat(\"https://primary.example\", http.request.uri.path)"
        },
        "preserve_query_string": true
      }
    }
  }]
}
```

This preserves path and query. PUT replaces the rules array: include existing rules you want to retain. [Entrypoint API](https://developers.cloudflare.com/api/resources/rulesets/subresources/phases/methods/update/), [update semantics](https://developers.cloudflare.com/ruleset-engine/rulesets-api/update/), [redirect parameters](https://developers.cloudflare.com/rules/url-forwarding/single-redirects/settings/).

Traffic must reach Cloudflare: POST to the secondary zone's DNS endpoint with `{"type":"A","name":"secondary.example","content":"192.0.2.1","ttl":1,"proxied":true}`. Alternatively use a proxied AAAA placeholder `100::`. Add a proxied `www` record and matching hostname expression if redirecting `www` too. HTTPS also requires an active edge certificate. [Domain redirect setup](https://developers.cloudflare.com/fundamentals/manage-domains/redirect-domain/).

## 5. Rate limits and errors

Default limits: 1,200 requests per five minutes per user/account token and 200 requests/second per IP. User limits aggregate dashboard, API-key, and token activity; endpoint-specific limits can also apply. On HTTP 429, honor `Retry-After`; monitor `Ratelimit` and `Ratelimit-Policy`. [Rate limits](https://developers.cloudflare.com/fundamentals/api/reference/limits/).

Inspect HTTP status and the JSON envelope: `{"success":true,"errors":[],"messages":[],"result":...}`. Failures report `success:false` and `errors[]` entries with numeric `code` and descriptive `message`; retain `errors[].code` for diagnostics. [Response schema](https://developers.cloudflare.com/api/resources/dns/subresources/records/methods/delete/).
