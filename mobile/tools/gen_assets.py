import json,os,glob,re
root='ios/Linbit/Resources/Assets.xcassets'
os.makedirs(root,exist_ok=True)
json.dump({"info":{"author":"xcode","version":1}},open(f'{root}/Contents.json','w'))
# 品牌文件（logo/mark/wordmark）不走通用单色图标流程，见下方 brand()
BRAND={'logo','mark','mark-lime','wordmark','wordmark-dark'}
for svg in glob.glob('icons/*.svg'):
    n=os.path.basename(svg)[:-4]; s=open(svg).read()
    if n in BRAND: continue
    d=f'{root}/ic-{n}.imageset'; os.makedirs(d,exist_ok=True)
    light=s.replace('currentColor','#080808')
    dark=s.replace('currentColor','#F5F5F5').replace('"#fff"','"#1E1E20"')
    open(f'{d}/{n}.svg','w').write(light); open(f'{d}/{n}-dark.svg','w').write(dark)
    json.dump({"images":[{"idiom":"universal","filename":f"{n}.svg"},
      {"idiom":"universal","filename":f"{n}-dark.svg","appearances":[{"appearance":"luminosity","value":"dark"}]}],
      "info":{"author":"xcode","version":1},"properties":{"preserves-vector-representation":True}},open(f'{d}/Contents.json','w'),indent=1)
# 品牌资源：ic-logo = 链环标志（日间深色 mark.svg / 夜间品牌绿 mark-lime.svg），
# ic-wordmark = 标志 + 「Linbit」（日间 wordmark.svg / 夜间 wordmark-dark.svg 去掉深色底，透明背景）
def brand(name,light,dark):
    d=f'{root}/ic-{name}.imageset'; os.makedirs(d,exist_ok=True)
    open(f'{d}/{name}.svg','w').write(open(f'icons/{light}.svg').read())
    open(f'{d}/{name}-dark.svg','w').write(re.sub(r'<rect\b[^>]*/>','',open(f'icons/{dark}.svg').read()))
    json.dump({"images":[{"idiom":"universal","filename":f"{name}.svg"},
      {"idiom":"universal","filename":f"{name}-dark.svg","appearances":[{"appearance":"luminosity","value":"dark"}]}],
      "info":{"author":"xcode","version":1},"properties":{"preserves-vector-representation":True}},open(f'{d}/Contents.json','w'),indent=1)
brand('logo','mark','mark-lime')
brand('wordmark','wordmark','wordmark-dark')
# app icon：AppIcon.png 不入库（.gitignore），由 tools/gen_app_icon.py 在构建前从 icons/logo.svg 生成
d=f'{root}/AppIcon.appiconset';os.makedirs(d,exist_ok=True)
json.dump({"images":[{"filename":"AppIcon.png","idiom":"universal","platform":"ios","size":"1024x1024"}],"info":{"author":"xcode","version":1}},open(f'{d}/Contents.json','w'))
print('ok')
