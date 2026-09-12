type JsonLDValue = null | boolean | number | string | JsonLDValue[] | { [key: string]: JsonLDValue | undefined };

function serializeJsonLD(value: JsonLDValue): string {
  return JSON.stringify(value)
    .replace(/</g, "\\u003c")
    .replace(/\u2028/g, "\\u2028")
    .replace(/\u2029/g, "\\u2029");
}

export function JsonLD({ data }: { data: JsonLDValue }) {
  return <script type="application/ld+json" dangerouslySetInnerHTML={{ __html: serializeJsonLD(data) }} />;
}
