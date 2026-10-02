#!/usr/bin/env python3
"""Generate Markdown reference and Postman v2.1 collection from JSON OpenAPI.

No third-party dependencies. Run from any directory; --check detects drift.
"""
import argparse
import json
import re
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
SPEC = json.loads((ROOT / 'docs/openapi.yaml').read_text())
METHODS = {'get', 'post', 'put', 'patch', 'delete', 'head', 'options'}
VARIABLES = {
    'base_url': 'http://localhost:8080', 'phone': '', 'otp_code': '',
    'phone_verification_key': '', 'trust_device_key': '', 'user_agent': 'ShopNext-Postman-device', 'pickup_code': '',
    'admin_access_token': '', 'staff_email': '', 'staff_password': '', 'new_staff_password': '',
    'new_staff_email': '', 'new_staff_name': 'Test staff',
    'product_slug': 'sample-product', 'product_id': '', 'variant_id': '',
    'image_id': '', 'category_id': '', 'provider_id': '', 'branch_id': '',
    'hero_slide_id': '', 'promo_banner_id': '', 'staff_id': '', 'refund_id': '',
    'bill_number': '', 'province': '', 'city': '', 'branch_name': '',
    'recipient_name': 'Test customer', 'payment_method': 'BCEL',
    'idempotency_key': '', 'media_url': '', 'filename': '',
    'provider_transaction_id': '', 'provider_refund_bill_id': '',
    'maintenance_secret': '', 'webhook_signature': '',
    'webhook_signature_header': 'x-phajay-signature',
}


def resolve(schema):
    if '$ref' in schema:
        value = SPEC
        for part in schema['$ref'].removeprefix('#/').split('/'):
            value = value[part]
        return value
    return schema


def group(path):
    if path.startswith('/admin/'):
        return 'Staff / ' + path.split('/')[2].replace('-', ' ').title()
    if path.startswith('/health/') or path == '/openapi.yaml':
        return 'Health and specification'
    if path.startswith('/otp/') or path == '/phone-verification':
        return 'Phone verification'
    if path.startswith('/bills') or path == '/orders':
        return 'Orders and bills'
    if path.startswith('/webhooks') or path.startswith('/maintenance'):
        return 'Callbacks and maintenance'
    if path.startswith('/media/'):
        return 'Media'
    return 'Shopping'


def path_variable(path, name):
    if name == 'slug':
        return 'product_slug'
    if name == 'bill':
        return 'bill_number'
    if name in ('variant', 'image'):
        return name + '_id'
    if name == 'id':
        resource = path.split('/')[2]
        return {'products': 'product_id', 'categories': 'category_id',
                'providers': 'provider_id', 'branches': 'branch_id',
                'hero-slides': 'hero_slide_id', 'promo-banners': 'promo_banner_id',
                'staff': 'staff_id', 'refunds': 'refund_id'}[resource]
    return name


def example(schema, field='', schema_name=''):
    if '$ref' in schema:
        return example(resolve(schema), field, schema['$ref'].split('/')[-1])
    if schema_name == 'StaffInput' and field == 'name':
        return '{{new_staff_name}}'
    if field == 'parent_id':
        return None
    if schema_name == 'Webhook' and field == 'status':
        return 'PAYMENT_COMPLETED'
    if field == 'password':
        return '{{staff_password}}' if schema_name == 'Login' else '{{new_staff_password}}'
    if field == 'email':
        return '{{staff_email}}' if schema_name == 'Login' else '{{new_staff_email}}'
    mapped = {'code': 'otp_code', 'recipient_phone': 'phone', 'bank': 'payment_method',
              'url': 'media_url', 'image_url': 'media_url', 'transactionId': 'provider_transaction_id',
              'billNumber': 'bill_number'}
    var = mapped.get(field, field)
    if var in VARIABLES:
        return '{{' + var + '}}'
    if 'example' in schema:
        return schema['example']
    if 'default' in schema:
        return schema['default']
    if 'const' in schema:
        return schema['const']
    if 'enum' in schema:
        return schema['enum'][0]
    for union in ('anyOf', 'oneOf'):
        if union in schema:
            return example(schema[union][0], field, schema_name)
    if 'allOf' in schema:
        result = {}
        for part in schema['allOf']:
            result.update(example(part))
        return result
    kind = schema.get('type', 'object')
    if isinstance(kind, list):
        kind = next(t for t in kind if t != 'null')
    if kind == 'object':
        return {k: example(v, k, schema_name) for k, v in schema.get('properties', {}).items()}
    if kind == 'array':
        return [example(schema.get('items', {}), field)]
    if kind in ('integer', 'number'):
        return max(schema.get('minimum', 0), 1)
    if kind == 'boolean':
        return True
    if kind == 'null':
        return None
    return {'name': 'Sample item', 'name_en': 'Sample item', 'sku': 'SAMPLE-001',
            'slug': 'sample-item', 'status': 'ACTIVE', 'reason': 'Customer requested refund',
            'note': 'Staff action', 'cta_href': '/products', 'role': 'SUPPORT'}.get(field, 'sample')


def type_label(schema):
    if '$ref' in schema:
        name = schema['$ref'].split('/')[-1]
        return f'[{name}](#{name.lower()})'
    for union in ('anyOf', 'oneOf', 'allOf'):
        if union in schema:
            return (' + ' if union == 'allOf' else ' or ').join(type_label(s) for s in schema[union])
    kind = schema.get('type', 'object')
    if isinstance(kind, list):
        kind = ' or '.join(kind)
    if kind == 'array':
        return 'array of ' + type_label(schema['items'])
    if kind == 'object' and schema.get('properties'):
        return 'object: ' + ', '.join('`' + k + '` ' + type_label(v) for k, v in schema['properties'].items())
    return kind + (f" ({schema['format']})" if 'format' in schema else '')


def constraints(schema):
    notes = [schema.get('description', '')]
    for key in ('enum', 'const', 'default', 'minimum', 'maximum', 'minLength', 'maxLength', 'minItems', 'maxItems', 'uniqueItems', 'pattern'):
        if key in schema:
            notes.append(f'{key}: `{json.dumps(schema[key], ensure_ascii=False)}`')
    return '; '.join(n for n in notes if n).replace('|', '&#124;').replace('\n', ' ')


def auth_label(operation):
    security = operation.get('security', SPEC.get('security', []))
    if not security:
        return 'Public'
    labels = {'PhoneVerificationKey': 'Phone verification header key', 'TrustDeviceKey': 'Order trusted-device header key', 'AdminBearer': 'Admin bearer JWT',
              'WebhookSignature': 'Webhook HMAC signature', 'MaintenanceBearer': 'Maintenance bearer secret'}
    return ' or '.join(' + '.join(labels[k] for k in s) if s else 'public (owner data requires verified phone)' for s in security)


def schema_fields(schema):
    schema = resolve(schema)
    props, required = dict(schema.get('properties', {})), set(schema.get('required', []))
    for part in schema.get('allOf', []):
        p, r = schema_fields(part)
        props.update(p)
        required.update(r)
    return props, required


def fields_table(schema):
    props, required = schema_fields(schema)
    if not props:
        return ['Type: ' + type_label(schema), '']
    lines = ['| Field | Type | Required | Notes |', '| --- | --- | --- | --- |']
    for name, prop in props.items():
        lines.append(f'| `{name}` | {type_label(prop)} | {"Yes" if name in required else "No"} | {constraints(prop)} |')
    return lines + ['']


def generate():
    md = ['# API endpoint reference', '',
          'Generated from [OpenAPI](openapi.yaml). Start with the [API guide](api.md) for authentication, workflows and Postman setup.', '',
          'Regenerate with `python3 tools/generate_api_docs.py`; verify with `python3 tools/generate_api_docs.py --check`.', '',
          'Paths use `/api/v1` unless stated otherwise. Required response fields describe presence, not whether their values can be null. Shared error responses are described in the guide.', '']
    folders = {}
    for path, operations in SPEC['paths'].items():
        for method, op in operations.items():
            if method not in METHODS:
                continue
            folder = group(path)
            folders.setdefault(folder, [])
            url_path = re.sub(r'\{([^}]+)\}', lambda m: '{{' + path_variable(path, m[1]) + '}}', path)
            # Media has its own root-level server override.
            prefix = '' if path.startswith('/media/') else '/api/v1'
            headers, query = [], []
            for param in op.get('parameters', []):
                if param['in'] == 'header':
                    value = {'X-ShopNext-CSRF': '1', 'Idempotency-Key': '{{idempotency_key}}', 'X-Phone-Verification-Key': '{{phone_verification_key}}', 'X-Trust-Device-Key': '{{trust_device_key}}', 'User-Agent': '{{user_agent}}'}.get(param['name'], '')
                    headers.append({'key': param['name'], 'value': value, 'description': param.get('description', ''), **({'disabled': True} if param['name'] == 'X-Trust-Device-Key' else {})})
                elif param['in'] == 'query':
                    value = example(param['schema'], param['name'])
                    if param['name'] == 'q':
                        value = '{{bill_number}}' if path == '/bills/search' else 'sample'
                    if param['name'] == 'order_id':
                        value = '{{bill_number}}'
                    query.append({'key': param['name'], 'value': str(value),
                                  'disabled': not param.get('required', False), 'description': constraints(param['schema'])})
            desc = op['summary'] + '\n\nAuthentication: ' + auth_label(op) + '.'
            if op.get('description'):
                desc += '\n\n' + op['description']
            request = {'method': method.upper(), 'header': headers, 'auth': {'type': 'noauth'},
                       'description': desc, 'url': {'raw': '{{base_url}}' + prefix + url_path,
                       'host': ['{{base_url}}'], 'path': (prefix + url_path).strip('/').split('/')}}
            active_query = [p for p in query if not p['disabled']]
            if query:
                request['url']['query'] = query
            if active_query:
                request['url']['raw'] += '?' + '&'.join(p['key'] + '=' + p['value'] for p in active_query)
            if any('AdminBearer' in scheme for scheme in op.get('security', [])):
                request['auth'] = {'type': 'bearer', 'bearer': [{'key': 'token', 'value': '{{admin_access_token}}', 'type': 'string'}]}
            if path == '/maintenance/release-expired':
                request['auth'] = {'type': 'bearer', 'bearer': [{'key': 'token', 'value': '{{maintenance_secret}}', 'type': 'string'}]}
            if path == '/webhooks/phajay':
                headers.append({'key': '{{webhook_signature_header}}', 'value': '{{webhook_signature}}'})
            content = op.get('requestBody', {}).get('content', {})
            if 'application/json' in content:
                body = example(content['application/json']['schema'])
                if path.endswith('/reorder'):
                    body['ids'] = ['{{' + ('category_id' if '/categories/' in path else 'variant_id' if '/variants/' in path else 'image_id' if '/images/' in path else 'hero_slide_id' if '/hero-slides/' in path else 'promo_banner_id') + '}}']
                request['body'] = {'mode': 'raw', 'raw': json.dumps(body, indent=2, ensure_ascii=False), 'options': {'raw': {'language': 'json'}}}
                headers.append({'key': 'Content-Type', 'value': 'application/json'})
            elif 'multipart/form-data' in content:
                request['body'] = {'mode': 'formdata', 'formdata': [{'key': 'file', 'type': 'file', 'src': [], 'description': 'Select a WebP file, maximum 5 MiB.'}]}
            item = {'name': method.upper() + ' ' + path + ' — ' + op['summary'], 'request': request, 'response': []}
            if path == '/orders' and method == 'post':
                item['event'] = [{'listen': 'test', 'script': {'type': 'text/javascript', 'exec': [
                    'if (pm.response.code === 201) {',
                    '  const data = pm.response.json().data;',
                    '  if (data && data.bill_number) pm.collectionVariables.set("bill_number", data.bill_number);',
                    '}']}}]
            folders[folder].append(item)
            md += [f'## {method.upper()} {prefix}{path}', '', op['summary'], '', '**Authentication:** ' + auth_label(op) + '.', '']
            if op.get('description'):
                md += [op['description'], '']
            if op.get('parameters'):
                md += ['| Parameter | Location | Required | Type | Notes |', '| --- | --- | --- | --- | --- |']
                for p in op['parameters']:
                    notes = (p.get('description', '') + ' ' + constraints(p['schema'])).strip().replace('|', '&#124;')
                    md.append(f'| `{p["name"]}` | {p["in"]} | {"Yes" if p.get("required") else "No"} | {type_label(p["schema"])} | {notes} |')
                md.append('')
            for mime, entry in content.items():
                md += ['**Request body:** `' + mime + '` (' + ('required' if op['requestBody'].get('required') else 'optional') + ').', '']
                md += fields_table(entry['schema'])
                if mime == 'application/json':
                    md += ['Example (replace `{{variables}}` with real values):', '', '```json', request['body']['raw'], '```', '']
            md += ['| Success status | Content type | Response |', '| --- | --- | --- |']
            for code, response in op['responses'].items():
                if not code.startswith('2'):
                    continue
                response = resolve(response)
                for mime, entry in response.get('content', {}).items():
                    md.append(f'| {code} | `{mime}` | {type_label(entry["schema"])} |')
            md.append('')
    md += ['## Data schemas', '', 'Response objects below use snake_case; provider webhook fields retain their provider casing.', '']
    for name, schema in SPEC['components']['schemas'].items():
        md += ['### ' + name, ''] + fields_table(schema)
        if any(k in schema for k in ('oneOf', 'anyOf')):
            md += ['```json', json.dumps(schema, indent=2, ensure_ascii=False), '```', '']
    collection = {'info': {'name': 'ShopNext Laos API',
        'schema': 'https://schema.getpostman.com/json/collection/v2.1.0/collection.json',
        'description': 'Generated from docs/openapi.yaml. See docs/api.md. Set base_url to the origin without /api/v1 or a trailing slash. Set customer verification/trust headers manually and copy the admin login access_token into a private admin_access_token variable. Fill variables before sending requests. Secrets are intentionally blank. Run requests individually: mutations change business state. Checkout captures bill_number only; copy customer verification/trust keys into private local variables manually. No credentials or OTPs are captured or logged.'},
        'variable': [{'key': k, 'value': v, 'type': 'string'} for k, v in VARIABLES.items()],
        'item': [{'name': name, 'item': items} for name, items in folders.items()]}
    return {'docs/api-reference.md': '\n'.join(md),
            'docs/shopnext-laos.postman_collection.json': json.dumps(collection, indent=2, ensure_ascii=False) + '\n'}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--check', action='store_true')
    args = parser.parse_args()
    stale = []
    for name, content in generate().items():
        path = ROOT / name
        if args.check:
            if not path.exists() or path.read_text() != content:
                stale.append(name)
        else:
            path.write_text(content)
            print('Generated ' + name)
    if stale:
        parser.exit(1, 'Outdated generated API docs: ' + ', '.join(stale) + '\n')
    if args.check:
        print('API reference and Postman collection match OpenAPI.')


if __name__ == '__main__':
    main()
