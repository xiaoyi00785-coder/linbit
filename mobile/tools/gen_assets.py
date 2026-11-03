import json,os,glob,re
root='ios/Linbit/Resources/Assets.xcassets'
os.makedirs(root,exist_ok=True)
json.dump({"info":{"author":"xcode","version":1}},open(f'{root}/Contents.json','w'))
for svg in glob.glob('icons/*.svg'):
    n=os.path.basename(svg)[:-4]; s=open(svg).read()
    d=f'{root}/ic-{n}.imageset'; os.makedirs(d,exist_ok=True)
    light=s.replace('currentColor','#080808')
    dark=s.replace('currentColor','#F5F5F5').replace('"#fff"','"#1E1E20"')
    open(f'{d}/{n}.svg','w').write(light); open(f'{d}/{n}-dark.svg','w').write(dark)
    json.dump({"images":[{"idiom":"universal","filename":f"{n}.svg"},
      {"idiom":"universal","filename":f"{n}-dark.svg","appearances":[{"appearance":"luminosity","value":"dark"}]}],
      "info":{"author":"xcode","version":1},"properties":{"preserves-vector-representation":True}},open(f'{d}/Contents.json','w'),indent=1)
# app icon：AppIcon.png 不入库（.gitignore），由 tools/gen_app_icon.py 在构建前从 icons/logo.svg 生成
d=f'{root}/AppIcon.appiconset';os.makedirs(d,exist_ok=True)
json.dump({"images":[{"filename":"AppIcon.png","idiom":"universal","platform":"ios","size":"1024x1024"}],"info":{"author":"xcode","version":1}},open(f'{d}/Contents.json','w'))
print('ok')
