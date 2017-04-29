#! /usr/bin/env python3

import math, colorsys

maxd = 20
go_list = []
for depth in range(maxd):
    (r, g, b) = colorsys.hsv_to_rgb(float(depth) / maxd, 0.65, 1.0)
    R, G, B = int(255 * r), int(255 * g), int(255 * b)
    start = "\033[38;2;%d;%d;%dm" % (R, G, B)
    end = "\033[0;0m"
    print("%s(%d %d %d)%s" % (start, R, G, B, end))
    go_list.append("RGBColor{%d,%d,%d}" % (R, G, B))

print("[]RGBColor{" + ",".join(go_list) + "}")
