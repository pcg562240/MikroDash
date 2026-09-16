import{readFileSync,writeFileSync,copyFileSync,existsSync}from'node:fs';
import{resolve,join,dirname}from'node:path';
import{fileURLToPath}from'node:url';
export function adapt(out){
 const root=resolve(dirname(fileURLToPath(import.meta.url)),'../..');out=resolve(out);
 if(out===root||out===join(root,'web'))throw Error('Traffic adapter must target an isolated translated copy');
 const file=join(out,'web/src/pages/dashboard.ts'),moduleFile=join(out,'web/src/pages/traffic-extension.ts');
 let text=readFileSync(file,'utf8');const anchor='export function initDashboard(socket: Socket): void {';
 if(text.split(anchor).length!==2||existsSync(moduleFile))throw Error('Traffic adapter anchor moved or adapter already applied: review upstream');
 text="import { initTrafficExtension } from './traffic-extension';\n"+text.replace(anchor,anchor+'\n  initTrafficExtension(socket);');
 writeFileSync(file,text);copyFileSync(join(root,'extensions/traffic/adapter.ts'),moduleFile);
}
if(process.argv[1]&&resolve(process.argv[1])===fileURLToPath(import.meta.url)){if(!process.argv[2])throw Error('Pass isolated translated output path');adapt(process.argv[2])}
