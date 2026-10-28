const {test}=require('node:test');
const assert=require('node:assert/strict');
const vm=require('node:vm');
const fs=require('node:fs');
const source=fs.readFileSync(__dirname+'/../web/app.js','utf8');
function fixture(){
 const values=new Map(),redirects=[];
 const ctx=vm.createContext({state:{bootstrap:{workspace:'https://workspace.example'},me:null},sessionStorage:{getItem:k=>values.get(k),setItem:(k,v)=>values.set(k,v),removeItem:k=>values.delete(k)},location:{hash:'#/edit/'+'a'.repeat(48),replace:u=>redirects.push(u)},fetch:async()=>({ok:false,status:401,json:async()=>({error:'Sign in to One Workspace',login:'/auth/login'})})});
 for(const name of ['api','loginURL','reconnectWorkspace'])vm.runInContext(source.split('\n').find(l=>l.startsWith('function '+name+'(')||l.startsWith('async function '+name+'(')),ctx);
 return {ctx,values,redirects};
}
test('Forms session with missing Workspace session reconnects through Workspace once',async()=>{
 const {ctx,values,redirects}=fixture();
 let error;try{await vm.runInContext("api('api/v1/forms')",ctx)}catch(e){error=e}
 ctx.error=error;
 assert.equal(vm.runInContext('reconnectWorkspace(error)',ctx),true);
 assert.deepEqual(redirects,['/auth/login?returnTo=%2Fforms%2F']);
 assert.equal(values.get('myforms-return'),'#/edit/'+'a'.repeat(48));
 assert.equal(vm.runInContext('reconnectWorkspace(error)',ctx),false);
});
test('sign-in uses Workspace session; ordinary failures do not redirect',()=>{
 const {ctx,redirects}=fixture();
 assert.equal(vm.runInContext('loginURL()',ctx),'/auth/login?returnTo=%2Fforms%2F');
 assert.equal(vm.runInContext('reconnectWorkspace({status:403})',ctx),false);
 assert.equal(vm.runInContext('reconnectWorkspace({status:401})',ctx),false);
 assert.equal(redirects.length,0);
});
