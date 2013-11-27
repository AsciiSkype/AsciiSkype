package main
//Pointless Comment

import (
    "flag"
    "fmt"
    "os"
    "math"
    "image"
    "strconv"
    "image/color"
    _ "image/png"
    _ "image/gif"
    _ "image/jpeg"
    _ "image/draw"
    "time"
)

/*******
Pipeline
*******/

/*
Parses command line arguments into a dictionary of key-value pairs and a list of filenames
*/
func parseArgs() (map[string]int, []string){
    var noBlur=flag.Bool("nb", false, "Don't apply the gaussian blur to image")
    var edgeDetectThreshold=flag.Int("t",100000, "Edge detection threshold")
    var particleThreshold=flag.Int("n",0, "Number of particles in window required for edge to be recognized")
    var pixelsPerX=flag.Int("x",1, "Number of x pixels per character")
    var pixelsPerY=flag.Int("y",2, "Number of y pixels per character")
    flag.Parse()

    if flag.NArg()<1{
        usage()
        die([]string{})
    }

    var flags = make(map[string]int)

    flags["edgeThreshold"]=(*edgeDetectThreshold)*(*edgeDetectThreshold)
    flags["particleThreshold"]=*particleThreshold
    flags["xWin"]=*pixelsPerX
    flags["yWin"]=*pixelsPerY
    if *noBlur{
        flags["blur"]=0
    }else{
        flags["blur"]=1
    }

    var filenames = make([]string,flag.NArg())
    for i:=0; i < flag.NArg(); i++{
        filenames[i]=flag.Arg(i)
    }
    return flags, filenames
}




/*
Loads an image and returns it
*/
func loadImg(path string) (image.Image){
    var file, openerr =os.Open(path)
    if openerr!=nil{
        die([]string{openerr.Error(),"Failed to open file "+path})
    }
    var img,_, decodeerr = image.Decode(file)
    if decodeerr!=nil{
        die([]string{decodeerr.Error(), "Failed to decode file "+path})
    }
    return img
}

/*
Converts an image to a grayscale row major value array, and returns the dimensions of the image
*/
func toGrayScale(src image.Image) ([]float64,int,int){
    var bounds = src.Bounds();
    var width, height = bounds.Max.X, bounds.Max.Y

    var out = make([]float64,width*height)

    for x := 0; x < width; x++{
        //TODO: Parallelize
        for y := 0; y < height; y++{
            var oldColor = src.At(x,y)
            var grayColor = color.GrayModel.Convert(oldColor)
            var v,_,_,_ = grayColor.RGBA()
            out[x+y*width]=float64(v)
        }
    }

    return out,width,height
}


/*
Applies the Gaussian low pass filter to the image
*/
func gaussianFilter(img []float64, width int, height int)([]float64){
    var gaussian = []float64{
        2.0/159.0,  4.0/159.0,  5.0/159.0,  4.0/159.0,  2.0/159.0,
        4.0/159.0,  9.0/159.0,  12.0/159.0, 9.0/159.0,  4.0/159.0,
        5.0/159.0,  12.0/159.0, 15.0/159.0, 12.0/159.0, 5.0/159.0,
        4.0/159.0,  9.0/159.0,  12.0/159.0, 9.0/159.0,  4.0/159.0,
        2.0/159.0,  4.0/159.0,  5.0/159.0,  4.0/159.0,  2.0/159.0,
    }
    return convolve(img,gaussian,width,height,5)
}

/*
Edge detects the image, returning a binary array of edge pixel locations
*/
func edgeDetect(img []float64,width int, height int, edgeThreshold int, blur bool)([]bool, []float64, []float64){
    var start, end time.Time
    start=time.Now()
    var blurred []float64
    //Apply sobel filter
    var sobelX = []float64{
        -1.0,0.0,1.0,
        -2.0,0.0,2.0,
        -1.0,0.0,1.0,
    }
    var sobelY = []float64{
        1.0,2.0,1.0,
        0.0,0.0,0.0,
        -1.0,-2.0,-1.0,
    }
    var edge = make([]bool,len(img))
    var magnitude = make([]float64,len(img))
    var angle = make([]float64,len(img))
    end=time.Now()
    println("\tTime taken by variable initialization:",end.Sub(start).Nanoseconds()/1000000,"ms")
    start=time.Now()
    if blur{
        blurred = gaussianFilter(img,width,height)
    }else{
        blurred = img
    }
    end=time.Now()
    println("\tTime taken by gaussian convolution:",end.Sub(start).Nanoseconds()/1000000,"ms")
    start=time.Now()
    var gX = convolve(blurred,sobelX,width,height,3)
    var gY = convolve(blurred,sobelY,width,height,3)
    end=time.Now()
    println("\tTime taken by sobel filter convolution:",end.Sub(start).Nanoseconds()/1000000,"ms")
    start=time.Now()
    for i := 0; i < len(img); i++{
        var normSquared=gX[i]*gX[i]+gY[i]*gY[i]
        angle[i]=math.Atan2(gX[i],gY[i])
        magnitude[i]=normSquared
        edge[i]=normSquared>float64(edgeThreshold)
    }
    end=time.Now()
    println("\tTime taken by sobel unpacking:",end.Sub(start).Nanoseconds()/1000000,"ms")
    start=time.Now()
    //Thin edges
    for i := 0; i < width; i++{
        for j := 0; j < height; j++{
            edge[i+width*j]=edge[i+width*j] && isLocalMax(i,j,width,height,magnitude,angle)
        }
    }
    end=time.Now()
    println("\tTime taken by edge thinning:",end.Sub(start).Nanoseconds()/1000000,"ms")
    return edge, magnitude, angle
}

/*
Takes a binary image, edge strengths, and edge angles and returns the ascii image
*/
func quantizeToAscii(edges []bool, magnitude []float64, angles []float64, width int, height int,  xWin int, yWin int, particleThreshold int)([]string){
    var xNum = width/xWin
    var yNum = height/yWin
    var out = make([]string,xNum*yNum)


    for y:=0; y<yNum; y++{
        for x:=0; x<xNum; x++{
            var blockIndex = x*xWin+y*yWin*width

            var angleTotal = 0.0
            var magnitudeTotal = 0.0
            var numTrue = 0

            for i:=0; i<xWin; i++{
                for j:=0; j<yWin; j++{
                    var offsetIndex = blockIndex+i+j*width
                    if edges[offsetIndex]{
                        numTrue++
                    }
                    magnitudeTotal += magnitude[offsetIndex]
                    angleTotal += angles[offsetIndex]*magnitude[offsetIndex]
                }
            }
            angleTotal /= magnitudeTotal
            if numTrue > particleThreshold{
                out[x+xNum*y]=angToChar(angleTotal)
            }else{
                out[x+xNum*y]=" "
            }
        }
    }
    return out
}

/*
Converts an array of strings to a single contiguous string
*/
func matToString(mat []string,charWidth int,charHeight int)(string){
    var outstr = ""
    for j:=0; j<charHeight; j++{
        for i:=0; i<charWidth; i++{
            outstr+=string(mat[i+j*charWidth])
        }
        outstr+="\n"
    }
    return outstr
}

/*
Chains filters to convert an image to an ascii line drawing
*/
func asciiize(path string,edgeThreshold int,particleThreshold int, blur bool,xWin int,yWin int)(string){
    var start, end time.Time
    start=time.Now()
    var src = loadImg(path)
    end=time.Now()
    println("Time taken by image load:",end.Sub(start).Nanoseconds()/1000000,"ms")
    start=time.Now()
    var gray,width,height = toGrayScale(src)
    end=time.Now()
    println("Time taken by grayscale conversion:",end.Sub(start).Nanoseconds()/1000000,"ms")
    var edge, magnitude, angle = edgeDetect(gray,width,height, edgeThreshold, blur)
    start=time.Now()
    var out = quantizeToAscii(edge, magnitude, angle, width, height, xWin, yWin, particleThreshold)
    end=time.Now()
    println("Time taken by ascii conversion:",end.Sub(start).Nanoseconds()/1000000,"ms")
    return matToString(out,width/xWin,height/yWin)
}

/************************************
Main (dunno why this needs a section)
************************************/

/*
Main... doh...
*/
func main(){
    var flags, fileNames = parseArgs()
    for i:=0; i<len(fileNames); i++{
        fmt.Println(asciiize(fileNames[i], flags["edgeThreshold"], flags["particleThreshold"], flags["blur"]==1, flags["xWin"], flags["yWin"]))
    }
}


/******************************
Helpers (ie non-pipeline stuff)
*******************************/

/*
Prints the usage message
*/
func usage(){
    println("Usage: go run asciiize.go [-t <edge threshold>] [-x <x window>] [-y <y window>] filename1 [filename2] [filename3] ...")
}

/*
Causes the program to end, printing any errors that caused unexpected termination.
*/
func die(errors []string){
    if len(errors)>0{
        var errormsg = "Script has terminated because of the following errors:\n"
        for i:=0; i<len(errors); i++{
            errormsg += "\t"+errors[i]+"\n"
        }
        fmt.Fprint(os.Stderr,errormsg)
    }
    os.Exit(1)
}

/*
Prints the image specified by a matrix
*/
func printImage(img []float64, width int, height int){
    for j:=0; j<height; j++{
        for i:=0; i<width; i++{
            fmt.Print(9-int(math.Floor(img[i+j*width]/7281)))
        }
        fmt.Println()
    }
}

/*
Prints the binary image specified by a matrix
*/
func printBinImage(img []bool, width int, height int){
    for j:=0; j<height; j++{
        for i:=0; i<width; i++{
            if img[i+j*width]{
                fmt.Print(1)
            }else{
                fmt.Print(0)
            }
        }
        fmt.Println()
    }
}

/*
Convolves one matrix with another
*/
func convolve(img []float64, filter []float64, width int, height int,filterDimension int) []float64{
    //TODO: implement fft
    var out=make([]float64,len(img))
    var filterSize = filterDimension*filterDimension;
    var halfFilterDimension=filterDimension/2
    for i:=0; i < len(img); i++{
        out[i]=0;
        var imgX=i%width
        var imgY=i/width
        for j:=0; j<filterSize; j++{
            x:=j%filterDimension-halfFilterDimension
            y:=j/filterDimension-halfFilterDimension
            if imgX+x<width && imgX+x>=0 && imgY+y<height && imgY+y>=0{
                out[i]+=filter[j]*img[i+width*y+x]
            }
        }
    }
    return out
}

/*
Returns the closest approximation line of an angle
*/
func angToChar(a float64)(string){
    var verticalConstant = 10.0
    var horizontalConstant = -10.0
    var deg =  radToDeg(a,0.0)
    if deg<0{
        deg=360+deg
    }
    //0 30 60 90 120 150 180 210 240 270 300 330 360
    switch{
    case (30-horizontalConstant <= deg && deg < 60+verticalConstant) || (210 - horizontalConstant <= deg && deg < 240+verticalConstant):
        return "\\"
        //Pointless Comment
    case (60+verticalConstant <= deg && deg < 120-verticalConstant) || (240+verticalConstant <= deg && deg < 300-verticalConstant):
        return "|"
    case (120-verticalConstant <= deg && deg < 150+horizontalConstant) || (300-verticalConstant <= deg && deg < 330+horizontalConstant):
        return "/"
    case (150+horizontalConstant <= deg && deg < 210-horizontalConstant) || (330+horizontalConstant <= deg && deg <= 360) || (0 <= deg && deg < 30-horizontalConstant):
        return "-"
    default:
        die([]string{"Illegal angle found in function angToChar"})
        return ""
    }
}

/*
Converts radians to degrees 
*/
func radToDeg(a float64, rotation float64) (float64){
    var deg = a*180/math.Pi

    if deg<0{
        deg=360+deg
    }

    deg+=rotation

    if deg>=360{
        deg-=360
    }

    return deg
}

/*
Determines whether a point is a local max in a gradient
*/
func isLocalMax(i int, j int, width int, height int, mag []float64, ang []float64)(bool){
    var deg = radToDeg(ang[i+width*j],90.0)
    var deltaX,deltaY int
    switch{
    case (0<=deg && deg<22.5 || 337.5<=deg && deg<=360):
        fallthrough //Horizontal Case
    case (157.5<=deg && deg<202.5):
        deltaX=1
        deltaY=0
    case (22.5<=deg && deg<67.5):
        fallthrough //Upper right diagonal Case
    case (202.5<=deg && deg<257.5):
        deltaX=1
        deltaY=1
    case (67.5<=deg && deg<112.5):
        fallthrough //Vertical Case
    case (257.5<=deg && deg<292.5):
        deltaX=0
        deltaY=1
    case (112.5<=deg && deg<157.5):
        fallthrough //Upper left diagonal Case
    case (292.5<=deg && deg<337.5):
        deltaX=-1
        deltaY=1
    default :
        die([]string{"Illegal angle found in function isLocalMax:"+strconv.FormatFloat(deg,'f',5,64)})
    }
    var centerVal=mag[i+width*j]
    var leftVal = 0.0
    var rightVal = 0.0
    if 0 < i-deltaX && i-deltaX < width && 0 < j-deltaY && j-deltaY < height {leftVal=mag[i-deltaX+(j-deltaY)*width]}
    if 0 < i+deltaX && i+deltaX < width && 0 < j+deltaY && j+deltaY < height {rightVal=mag[i+deltaX+(j+deltaY)*width]}
    return centerVal>leftVal && rightVal>leftVal
}
