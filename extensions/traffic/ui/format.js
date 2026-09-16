export function bytes(n){if(!Number.isFinite(n)||n<0)return '—';const u=['B','KiB','MiB','GiB','TiB'];let i=0;while(n>=1024&&i<u.length-1){n/=1024;i++}return `${n.toLocaleString('zh-CN',{maximumFractionDigits:i?2:0})} ${u[i]}`}
export function orderDevices(rows,key){const score=x=>key==='total'?x.download+x.upload:x[key];return [...rows].sort((a,b)=>score(b)-score(a)||a.mac.localeCompare(b.mac))}
export function stale(at,now=Date.now(),threshold=15000){return !at||now-at>threshold||at>now+5000}
