import { useEffect, useRef } from 'react'
import { createChart } from 'lightweight-charts'
import { candles } from './data'
const css=(n)=>getComputedStyle(document.documentElement).getPropertyValue(n).trim()
// 可复用 K 线组件：data 为 {time,open,high,low,close,v}[]，颜色读取 CSS 变量，便于换肤
export default function KChart({data,volume=true,className='chart',height}){const r=useRef();useEffect(()=>{
const ch=createChart(r.current,{autoSize:true,layout:{background:{color:css('--bg')},textColor:css('--text-2'),fontSize:11},grid:{vertLines:{color:css('--grid')},horzLines:{color:css('--grid')}},rightPriceScale:{borderColor:css('--card-2')},timeScale:{borderColor:css('--card-2'),timeVisible:true}});
const d=data||candles();ch.addCandlestickSeries({upColor:css('--up'),downColor:css('--down'),wickUpColor:css('--up'),wickDownColor:css('--down'),borderVisible:false}).setData(d)
ch.priceScale('right').applyOptions({scaleMargins:{top:.05,bottom:volume?.32:.05}})
if(volume){const v=ch.addHistogramSeries({priceScaleId:'v',priceFormat:{type:'volume'}});ch.priceScale('v').applyOptions({scaleMargins:{top:.72,bottom:0}});v.setData(d.map(c=>({time:c.time,value:c.v,color:c.close>=c.open?css('--up-vol'):css('--down-vol')})))}
ch.timeScale().fitContent();return()=>ch.remove()},[data,volume]);return <div ref={r} className={className} style={height?{height}:undefined}/>}
