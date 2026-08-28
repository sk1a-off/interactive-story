import '@testing-library/jest-dom/vitest'

Object.defineProperty(HTMLMediaElement.prototype,'play',{configurable:true,value:()=>Promise.resolve()})
Object.defineProperty(HTMLMediaElement.prototype,'pause',{configurable:true,value:()=>undefined})
Object.defineProperty(HTMLMediaElement.prototype,'load',{configurable:true,value:()=>undefined})
Object.defineProperty(URL,'createObjectURL',{configurable:true,value:()=>`blob:test-${Math.random()}`})
Object.defineProperty(URL,'revokeObjectURL',{configurable:true,value:()=>undefined})
