const D=require('better-sqlite3');
const db=new D('data/app.db');
console.log('users:',JSON.stringify(db.prepare('SELECT id,email,name FROM users').all()));
console.log('task count:',db.prepare('SELECT COUNT(*) c FROM tasks').get().c);
console.log('docs:',JSON.stringify(db.prepare('SELECT id,title FROM docs').all()));
console.log('chat count:',db.prepare('SELECT COUNT(*) c FROM chat_messages').get().c);
console.log('chat rows:',JSON.stringify(db.prepare('SELECT id,sender_name,text,vpn_node FROM chat_messages').all()));

