import type { ResSchema } from './events-hand';
import { zhCatalog } from './zh-catalog';

function display(text: string): string {
  return zhCatalog[text.replace(/\s+/g, ' ').trim()] ?? text;
}

/** Only source-owned UI metadata. Never touch names, keys, options, values,
 * permissions, showIf conditions, menu paths or router-provided records. */
export function localizeResourceSchema(schema: ResSchema): ResSchema {
  return {
    ...schema,
    label: display(schema.label),
    title: display(schema.title),
    fields: schema.fields?.map(field => ({
      ...field,
      label: display(field.label),
      ...(field.help ? { help: display(field.help) } : {}),
      ...(field.placeholder ? { placeholder: display(field.placeholder) } : {}),
    })) ?? schema.fields,
    actions: schema.actions?.map(action => ({ ...action, label: display(action.label) })) ?? schema.actions,
  };
}
